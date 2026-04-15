package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/gin-gonic/gin"
)

var (
	// Print body max length
	defaultMaxLength = 300
	defaultLogFrom   = ""

	// Ignore route list
	defaultIgnoreRoutes = map[string]struct{}{
		"/ping":   {},
		"/pong":   {},
		"/health": {},
	}

	emptyBody   = []byte("")
	contentMark = []byte(" ...... ")
)

// Option set the gin logger options.
type Option func(*options)

func defaultOptions() *options {
	return &options{
		maxLength:     defaultMaxLength,
		ignoreRoutes:  defaultIgnoreRoutes,
		requestIDFrom: 1, // 默认从 context 获取 request_id
		logFrom:       defaultLogFrom,
		logHeaders:    false,
	}
}

type options struct {
	maxLength        int
	ignoreRoutes     map[string]struct{}
	requestIDFrom    int // Deprecated: request_id is now automatically extracted from context
	logFrom          string
	logHeaders       bool                // 是否记录请求头
	sensitiveHeaders map[string]struct{} // 敏感请求头列表(不记录)
}

func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithMaxLen logger content max length
func WithMaxLen(maxLen int) Option {
	return func(o *options) {
		if maxLen < len(contentMark) {
			panic("maxLen should be greater than or equal to 8")
		}
		o.maxLength = maxLen
	}
}

func WithLogFrom(logFrom string) Option {
	return func(o *options) {
		o.logFrom = logFrom
	}
}

// WithIgnoreRoutes no logger content routes
func WithIgnoreRoutes(routes ...string) Option {
	return func(o *options) {
		for _, route := range routes {
			o.ignoreRoutes[route] = struct{}{}
		}
	}
}

// WithRequestIDFromContext name is field in context, default value is request_id
// Deprecated: request_id is now automatically extracted from context, this option is no longer needed
func WithRequestIDFromContext() Option {
	return func(o *options) {
		// 保留空实现以维持向后兼容
	}
}

// WithRequestIDFromHeader name is field in header, default value is X-Request-Id
// Deprecated: request_id is now automatically extracted from context, this option is no longer needed
func WithRequestIDFromHeader() Option {
	return func(o *options) {
		// 保留空实现以维持向后兼容
	}
}

// WithLogHeaders enable logging request headers
func WithLogHeaders() Option {
	return func(o *options) {
		o.logHeaders = true
	}
}

// WithSensitiveHeaders set sensitive headers that should not be logged
func WithSensitiveHeaders(headers ...string) Option {
	return func(o *options) {
		if o.sensitiveHeaders == nil {
			o.sensitiveHeaders = make(map[string]struct{})
		}
		for _, header := range headers {
			o.sensitiveHeaders[strings.ToLower(header)] = struct{}{}
		}
	}
}

// ------------------------------------------------------------------------------------------

type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// getResponseBody returns the response body, possibly truncated
func getResponseBody(buf *bytes.Buffer, maxLen int) []byte {
	l := buf.Len()
	if l == 0 {
		return emptyBody
	} else if l > maxLen {
		l = maxLen
	}

	// Use Bytes() instead of Read() to avoid modifying the buffer
	allBytes := buf.Bytes()
	if l == len(allBytes) {
		if l < maxLen {
			return allBytes
		}
		// Truncate and add mark
		return append(allBytes[:maxLen-len(contentMark)], contentMark...)
	}

	// If length is different, copy the required portion
	result := make([]byte, l)
	copy(result, allBytes[:l])
	if l == maxLen {
		// Truncate and add mark
		return append(result[:maxLen-len(contentMark)], contentMark...)
	}
	return result
}

// getRequestBody returns the request body, possibly truncated
func getRequestBody(buf *bytes.Buffer, maxLen int) []byte {
	l := buf.Len()
	if l == 0 {
		return []byte("")
	} else if l < maxLen {
		return buf.Bytes()
	}

	allBytes := buf.Bytes()
	result := make([]byte, maxLen)
	copy(result, allBytes)
	return append(result[:maxLen-len(contentMark)], contentMark...)
}

// filterHeaders filters out sensitive headers
func filterHeaders(headers map[string][]string, sensitive map[string]struct{}) map[string]string {
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		// Skip sensitive headers
		if _, found := sensitive[strings.ToLower(k)]; found {
			continue
		}
		result[k] = fmt.Sprint(v)
	}
	return result
}

// Logging print request and response info
func Logging(opts ...Option) gin.HandlerFunc {
	o := defaultOptions()
	o.apply(opts...)

	// Initialize sensitive headers map if needed
	if o.sensitiveHeaders == nil {
		o.sensitiveHeaders = make(map[string]struct{})
	}
	// Add common sensitive headers by default
	sensitiveDefaults := []string{}
	for _, header := range sensitiveDefaults {
		if _, exists := o.sensitiveHeaders[header]; !exists {
			o.sensitiveHeaders[header] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		start := time.Now()

		// ignore printing of the specified route
		if _, ok := o.ignoreRoutes[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		// print input information before processing
		var buf bytes.Buffer
		if c.Request.Body != nil {
			_, _ = buf.ReadFrom(c.Request.Body)
		}

		fields := []logger.Field{
			logger.String("method", c.Request.Method),
			logger.String("url", c.Request.URL.String()),
			logger.String("userAgent", c.Request.UserAgent()),
			logger.String("ip", c.ClientIP()),
		}

		// Add request headers to log fields if enabled
		if o.logHeaders {
			headers := filterHeaders(c.Request.Header, o.sensitiveHeaders)
			fields = append(fields, logger.Any("headers", headers))
		}

		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPatch || c.Request.Method == http.MethodDelete {
			// 获取请求内容类型
			contentType := c.Request.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, "multipart/form-data") {
				fields = append(fields,
					logger.Int("size", buf.Len()),
					logger.String("body", string(getRequestBody(&buf, o.maxLength))),
				)
			} else {
				fields = append(fields, logger.String("body", "form-data not logged"))
			}
		}
		
		// request_id 已由 extractContextFields 自动从 context 中提取，无需手动添加
		fields = append(fields, logger.String("log_from", `<<<<`+o.logFrom))

		logger.InfoWithCtx(c.Request.Context(), `gin middleware Logging`, fields...)

		if buf.Len() > 0 {
			c.Request.Body = io.NopCloser(&buf)
		} else {
			c.Request.Body = io.NopCloser(bytes.NewReader([]byte{}))
		}

		// replace writer
		newWriter := &bodyLogWriter{body: &bytes.Buffer{}, ResponseWriter: c.Writer}
		c.Writer = newWriter

		// processing requests
		c.Next()

		// print return message after processing
		fields = []logger.Field{
			logger.Int("code", c.Writer.Status()),
			logger.String("method", c.Request.Method),
			logger.String("url", c.Request.URL.Path),
			logger.String("ms", fmt.Sprintf("%v", float64(time.Since(start).Nanoseconds())/1e6)),
			logger.Int("size", newWriter.body.Len()),
			logger.String("response", string(getResponseBody(newWriter.body, o.maxLength))),
		}
		// request_id 已由 extractContextFields 自动从 context 中提取，无需手动添加
		fields = append(fields, logger.String("log_from", `>>>>`+o.logFrom))
		
		logger.InfoWithCtx(c.Request.Context(), `gin middleware Logging`, fields...)
	}
}
