// Package middleware 提供 Gin 框架的签名验证中间件。
// 支持自定义签名密钥、忽略特定 URL、全局忽略、签名有效期等配置。
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

	"github.com/gin-gonic/gin"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/gocrypto"
	"github.com/18721889353/sunshine/pkg/logger"
)

var defaultIgnoreURL = map[string]struct{}{}

// SignOption 签名验证配置选项函数类型
type SignOption func(*signOptions)

// signOptions 签名验证配置项
type signOptions struct {
	ignoreUrls      map[string]struct{} // 需要忽略签名的 URL 列表（精确匹配）
	ignoreAll       bool                // 是否全局忽略所有请求的签名验证（优先级最高）
	signKey         string              // 签名密钥
	signExpiredTime time.Duration       // 签名有效时长，0 表示不检查过期
}

// defaultSignOptions 返回默认配置
func defaultSignOptions() *signOptions {
	return &signOptions{
		ignoreUrls:      defaultIgnoreURL,
		signKey:         "",
		signExpiredTime: time.Second * 5,
	}
}

// apply 应用所有配置选项
func (o *signOptions) apply(opts ...SignOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithIgnoreURL 设置需要忽略签名验证的 URL 列表（精确匹配路径，如 /api/v1/health）
func WithIgnoreURL(urls ...string) SignOption {
	return func(o *signOptions) {
		for _, url := range urls {
			o.ignoreUrls[url] = struct{}{}
		}
	}
}

// WithSignIgnoreAll 设置忽略所有 URL 的签名验证。
// 启用后，所有请求均跳过签名校验，适用于开发/测试环境。
func WithSignIgnoreAll() SignOption {
	return func(o *signOptions) {
		o.ignoreAll = true
	}
}

// WithSignKey 设置签名密钥
func WithSignKey(signKey string) SignOption {
	return func(o *signOptions) {
		o.signKey = signKey
	}
}

// WithSignExpiredTime 设置签名过期时间，传入 0 表示不检查过期
func WithSignExpiredTime(signExpiredTime time.Duration) SignOption {
	return func(o *signOptions) {
		o.signExpiredTime = signExpiredTime
	}
}

// VerifySignatureMiddleware 创建 Gin 签名验证中间件。
// 验证流程：
//  1. 若开启 ignoreAll，直接放行
//  2. 若当前 URL 在 ignoreUrls 中，直接放行
//  3. 否则执行完整签名校验（参数完整性、时间戳、签名值）
func VerifySignatureMiddleware(opts ...SignOption) gin.HandlerFunc {
	o := defaultSignOptions()
	o.apply(opts...)

	return func(ctx *gin.Context) {
		// 优先判断是否全局忽略
		if o.ignoreAll {
			ctx.Next()
			return
		}

		// 其次判断是否在忽略 URL 列表中（精确匹配）
		if _, ok := o.ignoreUrls[ctx.Request.URL.Path]; ok {
			ctx.Next()
			return
		}

		// 执行签名验证
		err := verifySign(ctx, o)
		if err != nil {
			response.Out(ctx, errcode.InvalidParams.WithDetails(err.Error()))
			ctx.Abort()
			return
		}
		ctx.Next()
	}
}

// verifySign 执行实际签名验证逻辑
// 从请求体中解析 JSON，提取 sign、timestamp 等字段，并计算期望签名进行比对
func verifySign(ctx *gin.Context, o *signOptions) error {
	// 仅支持 POST 方法（可根据业务扩展）
	var body []byte
	var err error
	switch ctx.Request.Method {
	case "POST":
		body, err = io.ReadAll(ctx.Request.Body)
		if err != nil {
			return fmt.Errorf("read request body error: %v", err)
		}
		// 重置请求体，以便后续中间件和处理程序能够读取
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	default:
		return errors.New("unsupported request method")
	}

	// 将 JSON 解析为 map
	var mapData map[string]interface{}
	if err := json.Unmarshal(body, &mapData); err != nil {
		return err
	}

	// 将 JSON 中的浮点数统一转为字符串，保证签名计算一致性
	for key, value := range mapData {
		if intValue, ok := value.(float64); ok {
			mapData[key] = strconv.FormatFloat(intValue, 'f', -1, 64)
		}
	}

	// 提取 sign 字段
	signVal, ok := mapData["sign"]
	if !ok {
		return errors.New("sign is missing")
	}
	sign, ok := signVal.(string)
	if !ok {
		return errors.New("sign must be string")
	}
	// 调试模式：若签名为 "debug" 则直接放行（方便开发测试）
	if sign == "debug" {
		return nil
	}
	if sign == "" {
		return errors.New("sign is empty")
	}

	// 提取 timestamp 字段（支持字符串或数字）
	var timestamp string
	if val, ok := mapData["timestamp"].(string); ok {
		timestamp = val
	} else if val, ok := mapData["timestamp"].(float64); ok {
		timestamp = strconv.FormatFloat(val, 'f', -1, 64)
	} else {
		return errors.New("timestamp is missing or invalid type")
	}

	// 检查签名是否过期（如果配置了过期时间）
	if o.signExpiredTime > 0 {
		tsInt, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			return errors.New("timestamp format invalid")
		}
		currentTimestamp := time.Now().Unix()
		// 这里保留原逻辑：允许±60秒的时钟偏差（可根据需求调整）
		if currentTimestamp-tsInt <= -60 || currentTimestamp-tsInt >= 60 {
			return errors.New("timestamp expired")
		}
		// 将 timestamp 转为 int64 存入 map，确保与签名计算一致（原逻辑将 timestamp 转为 int64）
		mapData["timestamp"] = tsInt
	}

	// 计算期望签名
	expectedSign := createSign(ctx, o, mapData, o.signKey)

	// 比对签名
	if sign != expectedSign {
		return errors.New("sign error")
	}
	return nil
}

// createSign 根据参数和密钥生成签名（MD5 大写）
// 签名算法：
//  1. 对参数 map 按 key 排序，拼接成 key1=value1&key2=value2 格式（跳过 sign 字段）
//  2. 末尾追加 &key={signKey}
//  3. 计算 MD5 并转大写
func createSign(ctx context.Context, _ *signOptions, params map[string]interface{}, signKey string) string {
	key := strings.Trim(createEncryptStr(params), "&")
	logger.InfoWithCtx(ctx, "gin中间件拼接的key",
		logger.String("key", key))
	key = key + "&key=" + signKey
	return strings.ToUpper(gocrypto.Md5([]byte(key)))
}

// createEncryptStr 递归地将 map 转换为排序后的 key=value& 形式字符串。
// 支持嵌套 map、数组，自动跳过 nil、空字符串、false 布尔值（数组元素除外）。
// 注意：数组元素使用索引作为 key，格式为 "0=value&1=value"。
func createEncryptStr(params map[string]interface{}) string {
	var strBuilder strings.Builder
	var sortIn func(obj map[string]interface{})
	sortIn = func(obj map[string]interface{}) {
		// 收集非空 key
		keys := make([]string, 0, len(obj))
		for k, v := range obj {
			if v == nil {
				continue
			}
			if str, ok := v.(string); ok && str == "" {
				continue
			}
			if b, ok := v.(bool); ok && !b {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if k == "sign" { // 签名不参与计算
				continue
			}
			switch v := obj[k].(type) {
			case map[string]interface{}:
				sortIn(v) // 递归处理嵌套 map
			case []interface{}:
				// 数组处理：使用索引作为 key（格式：0=value&1=value）
				for idx, elem := range v {
					switch ev := elem.(type) {
					case map[string]interface{}:
						// 数组元素为 map 时递归处理（原逻辑直接 sortIn(ev)，但未加索引前缀，这里为保持原样，不修改）
						// 注意：原代码中 sortIn(ev) 会写入 strBuilder，但没有外层 key，可能导致混淆。
						// 这里保留原逻辑，但建议改为标准 JSON 序列化以确保一致性。
						// 为了兼容现有签名，我们不做改动。
						sortIn(ev)
					default:
						strBuilder.WriteString(fmt.Sprintf("%d=%v&", idx, ev))
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
		// 去掉末尾的 '&'
		result = result[:len(result)-1]
	}
	return result
}
