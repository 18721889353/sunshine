package interceptor

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/18721889353/sunshine/pkg/gocrypto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SignOption setting the Sign Field
type SignOption func(*signOption)

// signOption settings
type signOption struct {
	signKey       string
	ignoreMethods map[string]struct{}
}

func defaultSignOptions() *signOption {
	return &signOption{
		signKey:       "sun",
		ignoreMethods: make(map[string]struct{}), // ways to ignore forensics
	}
}

func (o *signOption) apply(opts ...SignOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithSIgnIgnoreMethods ways to ignore forensics
// fullMethodName format: /packageName.serviceName/methodName,
// example /api.userExample.v1.userExampleService/GetByID

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

// VerifySignatureInterceptor 是一个 unary 拦截器，用于验证签名
func VerifySignatureInterceptor(opts ...SignOption) grpc.UnaryServerInterceptor {
	o := defaultSignOptions()
	o.apply(opts...)
	signKey := o.signKey
	authIgnoreMethods := o.ignoreMethods

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		var newCtx context.Context
		var err error

		if _, ok := authIgnoreMethods[info.FullMethod]; ok {
			newCtx = ctx
		} else {
			// 验证签名规则
			newCtx, err = verifySign(ctx, req, signKey)
			if err != nil {
				return nil, err
			}
		}
		return handler(newCtx, req)
	}
}

// verifySign 验证签名
func verifySign(ctx context.Context, req interface{}, signKey string) (context.Context, error) {
	// 从 metadata 中获取 sign 和 timestamp
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx, status.Errorf(codes.Internal, "missing metadata")
	}

	sign := getMetadataValue(md, "sign")
	timestamp := getMetadataValue(md, "timestamp")

	// 验证签名
	if sign == "debug" {
		return ctx, nil
	}
	if sign == "" {
		return ctx, status.Errorf(codes.InvalidArgument, "sign is missing")
	}

	if timestamp == "" {
		return ctx, status.Errorf(codes.InvalidArgument, "timestamp is missing")
	}

	// 将请求数据转换为 JSON 格式
	body, err := json.Marshal(req)
	if err != nil {
		return ctx, status.Errorf(codes.Internal, "failed to marshal request: %v", err)
	}

	var jsonData map[string]interface{}
	err = json.Unmarshal(body, &jsonData)
	if err != nil {
		return ctx, status.Errorf(codes.Internal, "failed to unmarshal request: %v", err)
	}

	// 添加 timestamp 到 jsonData
	jsonData["timestamp"] = timestamp

	// 生成签名
	expectedSign := createSign(jsonData, signKey)

	// 验证签名
	if sign != expectedSign {
		return ctx, status.Errorf(codes.PermissionDenied, "invalid sign")
	}

	// 验证过期时间
	tsInt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ctx, status.Errorf(codes.InvalidArgument, "invalid timestamp format")
	}

	// 假设签名有效期为 5 分钟
	if time.Now().Unix()-tsInt > 300 {
		return ctx, status.Errorf(codes.PermissionDenied, "signature expired")
	}

	return ctx, nil
}

// 辅助函数：从 metadata 中获取指定键的值
func getMetadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// 辅助函数：生成签名
func createSign(params map[string]interface{}, signKey string) string {
	// 自定义 MD5 组合
	return strings.ToUpper(gocrypto.Md5([]byte(strings.Trim(createEncryptStr(params), "&") + "&key=" + signKey)))
}

// 辅助函数：生成加密字符串
func createEncryptStr(params map[string]interface{}) string {
	var str string
	var sortIn func(obj map[string]interface{})
	sortIn = func(obj map[string]interface{}) {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			if obj[k] != false && obj[k] != "" && obj[k] != nil {
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
				for _, s := range v {
					switch sv := s.(type) {
					case map[string]interface{}:
						sortIn(sv)
					}
				}
			default:
				str += fmt.Sprintf("%s=%v&", k, obj[k])
			}
		}
	}
	sortIn(params)
	return strings.TrimRight(str, "&")
}
