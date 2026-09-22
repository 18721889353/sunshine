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

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/logger"
)

// cachedUGCPolicy 包级缓存 UGCPolicy 实例，避免每次请求重新编译正则表达式。
// pprof 显示每次 UGCPolicy() 调用累积分配 ~45MB 内存（regexp/syntax.appendRange 等）。
// bluemonday.Policy 创建后为只读对象，Sanitize() 方法线程安全，可安全并发使用。
var cachedUGCPolicy = bluemonday.UGCPolicy()

// XSSOptions XSS 防护配置选项
type XSSOptions func(*xssOptions)

func defaultXSSOptions() *xssOptions {
	return &xssOptions{
		ignoreUrls: make(map[string]struct{}),
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

// WithIgnoreXSSURL 设置忽略 XSS 检查的 URL 列表（精确路径匹配）
func WithIgnoreXSSURL(urls ...string) XSSOptions {
	return func(o *xssOptions) {
		for _, url := range urls {
			o.ignoreUrls[url] = struct{}{}
		}
	}
}

// XSSCrossMiddleware XSS 跨站脚本攻击防护中间件
//
// 职责范围：对 JSON 请求体中所有字符串字段做 HTML 清洗，防止用户输入污染存储。
// 不在职责范围：
//   - 不校验 URL 协议（javascript:、data: 等），业务层拼接到 href/src 时须自行校验；
//   - 不做响应体转义，防止存储型 XSS 回显由响应层负责。
func XSSCrossMiddleware(opts ...XSSOptions) gin.HandlerFunc {
	o := defaultXSSOptions()
	o.apply(opts...)
	return func(ctx *gin.Context) {
		if _, ok := o.ignoreUrls[ctx.Request.URL.Path]; ok {
			ctx.Next()
			return
		}
		if err := xssCross(ctx); err != nil {
			response.Out(ctx, errcode.InvalidParams.WithOutMsg(err.Error()))
			ctx.Abort()
			return
		}
		ctx.Next()
	}
}

func xssCross(ctx *gin.Context) error {
	if ctx.Request.Body == nil {
		return nil
	}

	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		logger.WarnWithCtx(ctx.Request.Context(), "io.ReadAll error", logger.Err(err))
		// ReadAll 失败时原 body 已被部分消费，无法恢复。
		// 设置为空 body 并返回错误，由中间件 Abort 终止请求。
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(nil))
		return err
	}

	// 空 body 直接放行
	if len(body) == 0 {
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		return nil
	}

	// 关键：先跳过前导空白再判断首字节，否则以空格 / \t / \r / \n 开头的合法 JSON
	// 会被误判为非对象 / 数组，从而完全绕过 XSS 过滤。
	// 仅使用 JSON 规范允许的四种空白字符，不引入 Unicode 空白，避免过度 trim。
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 ||
		(trimmed[0] != '{' && trimmed[0] != '[') ||
		!json.Valid(body) {
		// 非 JSON 对象 / 数组、或非法 JSON，原样放行
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		return nil
	}

	sanitizedBody, err := sanitizeJSONEfficient(ctx.Request.Context(), body)
	if err != nil {
		logger.WarnWithCtx(ctx.Request.Context(), "sanitize JSON error", logger.Err(err))
		// 出错时恢复原 body，交给后续业务逻辑处理，避免把请求"吞掉"
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		return err
	}

	ctx.Request.Body = io.NopCloser(bytes.NewBuffer(sanitizedBody))
	return nil
}

// sanitizeJSONEfficient 高效处理 JSON 的 XSS 过滤
func sanitizeJSONEfficient(ctx context.Context, body []byte) ([]byte, error) {
	// 使用包级缓存的 policy，避免每次请求调用 bluemonday.UGCPolicy() 重新编译正则表达式
	policy := cachedUGCPolicy

	jsonResult := gjson.ParseBytes(body)

	// 防御性断言：上游虽已保证首字符是 { 或 [，但 gjson 对极端畸形输入可能返回空 Result。
	// 一旦出现，原样返回，避免把请求 body 搞坏。
	if !jsonResult.IsObject() && !jsonResult.IsArray() {
		return body, nil
	}

	result, err := sanitizeJSONValue(ctx, jsonResult, policy)
	if err != nil {
		return nil, err
	}
	return []byte(result), nil
}

// sanitizeJSONValue 递归处理 JSON 值并输出合法 JSON 字符串。
// 使用 strings.Builder 手工构造，避免 sjson 路径解析与 O(n²) 字符串拼接。
func sanitizeJSONValue(ctx context.Context, value gjson.Result, policy *bluemonday.Policy) (string, error) {
	switch {
	case value.IsObject():
		var b strings.Builder
		b.Grow(64)
		b.WriteByte('{')

		first := true
		var processErr error

		value.ForEach(func(key, val gjson.Result) bool {
			processedVal, err := sanitizeJSONValue(ctx, val, policy)
			if err != nil {
				processErr = err
				logger.WarnWithCtx(ctx, "process object value error",
					logger.String("key", key.String()), logger.Err(err))
				return false
			}

			if !first {
				b.WriteByte(',')
			}
			first = false

			// key 使用 json.Marshal 编码，保证引号、转义、unicode 正确
			kb, err := json.Marshal(key.Str)
			if err != nil {
				processErr = err
				logger.WarnWithCtx(ctx, "marshal object key error",
					logger.String("key", key.String()), logger.Err(err))
				return false
			}
			b.Write(kb)
			b.WriteByte(':')
			b.WriteString(processedVal)
			return true
		})

		if processErr != nil {
			return "", processErr
		}
		b.WriteByte('}')
		return b.String(), nil

	case value.IsArray():
		var b strings.Builder
		b.Grow(64)
		b.WriteByte('[')

		first := true
		var processErr error

		value.ForEach(func(_, val gjson.Result) bool {
			processedVal, err := sanitizeJSONValue(ctx, val, policy)
			if err != nil {
				// 错误必须向上传播，不能静默丢弃剩余元素
				processErr = err
				logger.WarnWithCtx(ctx, "process array value error", logger.Err(err))
				return false
			}

			if !first {
				b.WriteByte(',')
			}
			first = false
			b.WriteString(processedVal)
			return true
		})

		if processErr != nil {
			return "", processErr
		}
		b.WriteByte(']')
		return b.String(), nil

	case value.Type == gjson.String:
		s := value.String()

		// 快路径：无危险字符时跳过 Sanitize，避免所有字符串都跑正则链
		if containsDangerousChar(s) {
			s = policy.Sanitize(s)
		}

		encoded, err := marshalStringNoHTMLEscape(s)
		if err != nil {
			logger.WarnWithCtx(ctx, "json marshal string error", logger.Err(err))
			return `""`, nil
		}
		return encoded, nil

	case value.Type == gjson.Number:
		return value.Raw, nil

	case value.Type == gjson.True, value.Type == gjson.False, value.Type == gjson.Null:
		return value.Raw, nil

	default:
		// 防御性兜底：正常不会走到（上面已覆盖 gjson 全部类型）。
		// 若触发，按字符串清洗处理，避免直接透传未经清洗的原始数据。
		s := policy.Sanitize(value.String())
		encoded, err := marshalStringNoHTMLEscape(s)
		if err != nil {
			return `""`, nil
		}
		return encoded, nil
	}
}

// containsDangerousChar 快速判断字符串是否可能含 XSS 载荷，
// 用于跳过绝大多数"干净"字符串的 Sanitize 开销。
// 覆盖范围：尖括号（HTML 标签）、&（HTML 实体）、引号（属性逃逸）。
//
// 注意：此快路径不会拦截 "javascript:"、"data:" 等无特殊字符的 URL 协议，
// 这类载荷需由业务层在拼接 href / src 等敏感上下文时另行校验。
func containsDangerousChar(s string) bool {
	return strings.ContainsAny(s, "<>&\"'")
}

// marshalStringNoHTMLEscape 序列化字符串为 JSON，
// 并关闭 json.Marshal 默认的 HTML 转义（< > & 会被转成 \u003c 等）。
// 由于字符串已经过 bluemonday Sanitize，无需再次 HTML 转义。
func marshalStringNoHTMLEscape(s string) (string, error) {
	var buf bytes.Buffer
	buf.Grow(len(s) + 2)

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return "", err
	}

	// json.Encoder.Encode 会在末尾追加换行符，去掉它
	out := buf.Bytes()
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return string(out), nil
}
