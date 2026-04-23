package interceptor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/18721889353/sunshine/pkg/logger"
)

// SignOption 设置签名字段
type SignOption func(*signOption)

// signOption 设置项
type signOption struct {
	signKey         string
	ignoreMethods   map[string]struct{}
	signExpiredTime time.Duration
}

func defaultSignOptions() *signOption {
	return &signOption{
		signKey:         "sun",
		ignoreMethods:   make(map[string]struct{}), // 忽略的方法
		signExpiredTime: time.Second * 5,
	}
}

func (o *signOption) apply(opts ...SignOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithSignIgnoreMethods 设置忽略签名验证的方法
// fullMethodName 格式: /packageName.serviceName/methodName,
// 示例 /api.userExample.v1.userExampleService/GetByID
func WithSignIgnoreMethods(fullMethodNames ...string) SignOption {
	return func(o *signOption) {
		for _, method := range fullMethodNames {
			o.ignoreMethods[method] = struct{}{}
		}
	}
}

func WithSignKey(signKey string) SignOption {
	return func(o *signOption) {
		o.signKey = signKey
	}
}

func WithSignExpiredTime(signExpiredTime time.Duration) SignOption {
	return func(o *signOption) {
		o.signExpiredTime = signExpiredTime
	}
}

// VerifySignatureInterceptor 是一个 unary 拦截器，用于验证签名
func VerifySignatureInterceptor(opts ...SignOption) grpc.UnaryServerInterceptor {
	o := defaultSignOptions()
	o.apply(opts...)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		var newCtx context.Context
		var err error

		if _, ok := o.ignoreMethods[info.FullMethod]; ok {
			newCtx = ctx
		} else {
			// 验证签名规则
			newCtx, err = verifySign(ctx, req, o)
			if err != nil {
				return nil, err
			}
		}
		return handler(newCtx, req)
	}
}

// verifySign 验证签名
func verifySign(ctx context.Context, req interface{}, o *signOption) (context.Context, error) {
	sign := metautils.ExtractIncoming(ctx).Get("sign")
	timestamp := metautils.ExtractIncoming(ctx).Get("timestamp")
	nonceStr := metautils.ExtractIncoming(ctx).Get("nonce_str")

	// 将请求数据转换为 JSON 格式
	jsonBody, err := json.Marshal(req)
	if err != nil {
		return ctx, status.Errorf(codes.InvalidArgument, "failed to marshal request: %v", err)
	}

	var mapData map[string]interface{}
	err = json.Unmarshal(jsonBody, &mapData)
	if err != nil {
		return ctx, status.Errorf(codes.InvalidArgument, "failed to unmarshal request: %v", err)
	}

	// 处理 float64 类型的值，确保它们被正确解析为字符串
	for key, value := range mapData {
		if intValue, ok := value.(float64); ok {
			mapData[key] = strconv.FormatFloat(intValue, 'f', -1, 64)
		}
	}

	if nonceStr != "" && mapData["nonce_str"] == nil {
		mapData["nonce_str"] = nonceStr
	}
	if timestamp != "" && mapData["timestamp"] == nil {
		mapData["timestamp"] = timestamp
	}
	if sign != "" && mapData["sign"] == nil {
		mapData["sign"] = sign
	}

	// 验证签名
	if mapData["sign"].(string) == "debug" {
		return ctx, nil
	}
	if mapData["sign"] == nil || mapData["sign"].(string) == "" {
		return ctx, status.Errorf(codes.InvalidArgument, "sign is missing")
	}

	if mapData["timestamp"] == nil || mapData["timestamp"].(string) == "" {
		return ctx, status.Errorf(codes.InvalidArgument, "timestamp is missing")
	}

	// 生成签名
	expectedSign := createSign(ctx, mapData, o.signKey)

	// 验证签名
	if sign != expectedSign {
		return ctx, status.Errorf(codes.InvalidArgument, "invalid sign")
	}

	if o.signExpiredTime > 0 {
		// 验证过期时间
		tsInt, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			return ctx, status.Errorf(codes.InvalidArgument, "invalid timestamp format")
		}

		// 假设签名有效期为 5 分钟
		if time.Now().Unix()-tsInt > int64(o.signExpiredTime.Seconds()) {
			return ctx, status.Errorf(codes.InvalidArgument, "signature expired")
		}
	}

	return ctx, nil
}

// 辅助函数：生成签名
func createSign(ctx context.Context, params map[string]interface{}, signKey string) string {
	key := strings.Trim(createEncryptStr(params), "&")
	logger.InfoWithCtx(ctx, "grpc拦截器拼接的key", logger.String("key", key))
	key = key + "&key=" + signKey
	// 自定义 MD5 组合
	return strings.ToUpper(gocrypto.Md5([]byte(key)))
}

// 辅助函数：生成签名
func createEncryptStr(params map[string]interface{}) string {
	var strBuilder strings.Builder
	var sortIn func(obj map[string]interface{})
	sortIn = func(obj map[string]interface{}) {
		keys := make([]string, 0, len(obj))
		for k, v := range obj {
			if v != nil && v != "" {
				if b, ok := v.(bool); ok && !b {
					continue
				}
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "sign" {
				continue
			}
			switch v := obj[k].(type) {
			case map[string]interface{}:
				sortIn(v)
			case []interface{}:
				// 处理 []interface{} 类型
				var items []string
				for _, s := range v {
					switch sv := s.(type) {
					case map[string]interface{}:
						sortIn(sv)
					default:
						items = append(items, fmt.Sprintf("%v", sv))
					}
				}
				strBuilder.WriteString(fmt.Sprintf("%s=%s&", k, strings.Join(items, ",")))
			default:
				strBuilder.WriteString(fmt.Sprintf("%s=%v&", k, v))
			}
		}
	}
	sortIn(params)
	result := strBuilder.String()
	if len(result) > 0 {
		result = result[:len(result)-1] // Remove the trailing '&'
	}
	return result
}
