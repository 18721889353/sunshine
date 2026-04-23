package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/microcosm-cc/bluemonday"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/logger"
)

type XSSOptions func(*xssOptions)

func defaultXSSOptions() *xssOptions {
	return &xssOptions{
		ignoreUrls: map[string]struct{}{},
	}
}

type xssOptions struct {
	ignoreUrls map[string]struct{}
}

func (o *xssOptions) apply(opts ...XSSOptions) {
	for _, opt := range opts {
		opt(o)
	}
}

func WithIgnoreXSSURL(urls ...string) XSSOptions {
	return func(o *xssOptions) {
		for _, url := range urls {
			o.ignoreUrls[url] = struct{}{}
		}
	}
}

func XSSCrossMiddleware(opts ...XSSOptions) gin.HandlerFunc {
	o := defaultXSSOptions()
	o.apply(opts...)
	return func(ctx *gin.Context) {
		if _, ok := o.ignoreUrls[ctx.Request.URL.Path]; ok {
			ctx.Next()
			return
		}
		if err := xssCross(ctx, o); err != nil {
			response.Out(ctx, errcode.InvalidParams.WithOutMsg(err.Error()))
			ctx.Abort()
			return
		}
		// 继续处理请求
		ctx.Next()
	}
}

func xssCross(ctx *gin.Context, _ *xssOptions) error {
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		logger.WarnWithCtx(ctx.Request.Context(), "io.ReadAll error", logger.Err(err))
		return err
	}

	// 检查是否为JSON格式
	if !gjson.ValidBytes(body) {
		// 如果不是JSON，直接返回原body
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		return nil
	}

	// 使用更高效的方式处理JSON，避免递归中的性能问题
	sanitizedBody, err := sanitizeJSONEfficient(ctx.Request.Context(), body)
	if err != nil {
		logger.WarnWithCtx(ctx.Request.Context(), "sanitize JSON error", logger.Err(err))
		return err
	}

	// 重置请求体
	ctx.Request.Body = io.NopCloser(bytes.NewBuffer(sanitizedBody))
	return nil
}

// 高效处理JSON的XSS过滤
func sanitizeJSONEfficient(ctx context.Context, body []byte) ([]byte, error) {
	policy := bluemonday.UGCPolicy()

	// 使用gjson解析JSON并保持字段顺序
	jsonResult := gjson.ParseBytes(body)

	// 递归处理JSON并使用sjson构建结果以保持键顺序
	result, err := sanitizeJSONWithSJSON(ctx, jsonResult, policy)
	if err != nil {
		return nil, err
	}

	return []byte(result), nil
}

// 递归处理JSON值并使用sjson构建结果以保持键顺序
func sanitizeJSONWithSJSON(ctx context.Context, value gjson.Result, policy *bluemonday.Policy) (string, error) {
	switch {
	case value.IsObject():
		result := "{}"
		var processErr error

		value.ForEach(func(key, val gjson.Result) bool {
			processedVal, err := sanitizeJSONWithSJSON(ctx, val, policy)
			if err != nil {
				processErr = err
				logger.WarnWithCtx(ctx, "process object value error", logger.String("key", key.String()), logger.Err(err))
				return false
			}

			// 使用sjson设置值以保持键顺序
			result, err = sjson.SetRaw(result, key.Str, processedVal)
			if err != nil {
				processErr = err
				logger.WarnWithCtx(ctx, "sjson set raw error", logger.String("key", key.String()), logger.Err(err))
				return false
			}
			return true
		})

		if processErr != nil {
			return "", processErr
		}
		return result, nil

	case value.IsArray():
		// 先收集所有处理后的数组元素
		var processedItems []string
		value.ForEach(func(_, val gjson.Result) bool {
			processedVal, err := sanitizeJSONWithSJSON(ctx, val, policy)
			if err != nil {
				logger.WarnWithCtx(ctx, "process array value error", logger.Err(err))
				return false
			}
			processedItems = append(processedItems, processedVal)
			return true
		})

		// 构建数组JSON
		result := "["
		for i, item := range processedItems {
			if i > 0 {
				result += ","
			}
			result += item
		}
		result += "]"
		return result, nil

	case value.Type == gjson.String:
		cleaned := strings.TrimSpace(value.String())
		// 对所有字符串进行XSS过滤以确保安全
		cleaned = policy.Sanitize(cleaned)
		// 使用json.Marshal确保正确的JSON编码
		encoded, err := json.Marshal(cleaned)
		if err != nil {
			logger.WarnWithCtx(ctx, "json marshal string error", logger.Err(err))
			return `""`, nil // 返回空字符串作为fallback
		}
		return string(encoded), nil

	case value.Type == gjson.Number:
		return value.Raw, nil

	case value.Type == gjson.True:
		return "true", nil

	case value.Type == gjson.False:
		return "false", nil

	case value.Type == gjson.Null:
		return "null", nil

	default:
		// 对于未知类型，安全地返回原始JSON
		if value.Exists() {
			return value.Raw, nil
		}
		return "null", nil
	}
}
