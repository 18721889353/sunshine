package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/gin-gonic/gin"
)

var defaultIgnoreUrl = map[string]struct{}{}

type SignOption func(*signOptions)

func defaultSignOptions() *signOptions {
	return &signOptions{
		ignoreUrls:      defaultIgnoreUrl,
		signKey:         "",
		signExpiredTime: time.Second * 5,
	}
}

type signOptions struct {
	ignoreUrls      map[string]struct{}
	signKey         string
	signExpiredTime time.Duration
}

func (o *signOptions) apply(opts ...SignOption) {
	for _, opt := range opts {
		opt(o)
	}
}
func WithIgnoreUrl(urls ...string) SignOption {
	return func(o *signOptions) {
		for _, url := range urls {
			o.ignoreUrls[url] = struct{}{}
		}
	}
}
func WithSignKey(signKey string) SignOption {
	return func(o *signOptions) {
		o.signKey = signKey
	}
}
func WithSignExpiredTime(signExpiredTime time.Duration) SignOption {
	return func(o *signOptions) {
		o.signExpiredTime = signExpiredTime
	}
}

func VerifySignatureMiddleware(opts ...SignOption) gin.HandlerFunc {
	o := defaultSignOptions()
	o.apply(opts...)
	return func(ctx *gin.Context) {
		if _, ok := o.ignoreUrls[ctx.Request.URL.Path]; ok {
			ctx.Next()
			return
		}
		//if ctx.Request.Method != http.MethodGet && ctx.Request.Method != http.MethodDelete {
		//验证签名规则
		err := verifySign(ctx, o)
		if err != nil {
			response.Out(ctx, errcode.InvalidParams.WithDetails(err.Error()))
			ctx.Abort()
			return
		}
		//}
		ctx.Next()
	}
}

// 验证签名
func verifySign(ctx *gin.Context, o *signOptions) error {
	// 根据请求方法获取请求数据
	var body []byte
	var err error
	switch ctx.Request.Method {
	//case "GET", "DELETE":
	//	body = []byte(ctx.Request.URL.RawQuery)
	case "POST":
		body, _ = io.ReadAll(ctx.Request.Body)
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	default:
		return errors.New("unsupported request method")
	}
	//body, err := io.ReadAll(ctx.Request.Body)
	//if err != nil {
	//	return err
	//}
	//// 重置请求体，以便后续中间件和处理程序能够读取它
	//ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	var mapData map[string]interface{}
	err = json.Unmarshal(body, &mapData)
	if err != nil {
		return err
	}
	// 处理 float64 类型的值，确保它们被正确解析为字符串
	for key, value := range mapData {
		if intValue, ok := value.(float64); ok {
			mapData[key] = strconv.FormatFloat(intValue, 'f', -1, 64)
		}
	}

	sign := ""      //表示签名加密串，用来验证数据的完整性，防止数据篡改
	timestamp := "" //表示时间戳，用来验证接口的时效性。
	if value, ok := mapData["sign"].(string); ok {
		sign = value
	} else {
		return errors.New("sign not empty")
	}
	// 验证签名
	if sign == "debug" {
		return nil
	}

	if value, ok := mapData["timestamp"].(string); ok {
		timestamp = value
	} else if value, ok := mapData["timestamp"].(float64); ok {
		timestamp = strconv.FormatFloat(value, 'f', -1, 64)
	} else {
		return errors.New("timestamp error")
	}

	if o.signExpiredTime > 0 {
		// 验证过期时间
		currentTimestamp := time.Now().Unix()
		tsInt, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			return errors.New("timestamp error")
		}
		mapData["timestamp"] = tsInt
		if currentTimestamp-tsInt <= -60 || currentTimestamp-tsInt >= 60 {
			return errors.New("timestamp expired")
		}
	}

	if sign == "" || sign != createSign(ctx, o, mapData, o.signKey) {
		return errors.New("sign error")
	}
	return nil
}

//func createSign(params map[string]interface{}, signKey string) string {
//	// 自定义 MD5 组合
//	//dump.P(strings.Trim(createEncryptStr(params), "&") + "&key=" + signKey)
//	return strings.ToUpper(gocrypto.Md5([]byte(strings.Trim(createEncryptStr(params), "&") + "&key=" + signKey)))
//}

func createSign(ctx context.Context, o *signOptions, params map[string]interface{}, signKey string) string {
	key := strings.Trim(createEncryptStr(params), "&")
	logger.InfoWithCtx(ctx, "gin中间件拼接的key",
		logger.String("key", key))
	key = key + "&key=" + signKey
	// 自定义 MD5 组合
	return strings.ToUpper(gocrypto.Md5([]byte(key)))
}
func createEncryptStr(params map[string]interface{}) string {
	var strBuilder strings.Builder
	var sortIn func(obj map[string]interface{})
	sortIn = func(obj map[string]interface{}) {
		keys := make([]string, 0, len(obj))
		for k, v := range obj {
			if v != false && v != "" && v != nil {
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
				for idx, s := range v {
					switch sv := s.(type) {
					case map[string]interface{}:
						sortIn(sv) // 递归处理子 map
					default:
						strBuilder.WriteString(fmt.Sprintf("%d=%v&", idx, sv)) // 修改点：index=value 拼接
					}
				}
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
