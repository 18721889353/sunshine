package middleware

import (
	"context"
	"net/http"

	"github.com/bwmarrin/snowflake"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/18721889353/sunshine/pkg/krand"
	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	// HeaderXRequestIDKey header request id key
	HeaderXRequestIDKey = "X-Request-Id"
)

// RequestIDOption set the request id  options.
type RequestIDOption func(*requestIDOptions)

type requestIDOptions struct {
	contextRequestIDKey string
	headerXRequestIDKey string
	snow                *snowflake.Node
}

func defaultRequestIDOptions() *requestIDOptions {
	return &requestIDOptions{
		contextRequestIDKey: string(logger.ContextKeyRequestID),
		headerXRequestIDKey: HeaderXRequestIDKey,
	}
}

func (o *requestIDOptions) apply(opts ...RequestIDOption) {
	for _, opt := range opts {
		opt(o)
	}
}

func (o *requestIDOptions) setRequestIDKey() {
	if o.headerXRequestIDKey != HeaderXRequestIDKey {
		HeaderXRequestIDKey = o.headerXRequestIDKey
	}
}

// WithContextRequestIDKey set context request id key, minimum length of 4
func WithContextRequestIDKey(key string) RequestIDOption {
	return func(o *requestIDOptions) {
		if len(key) < 4 {
			return
		}
		o.contextRequestIDKey = key
	}
}

// WithHeaderRequestIDKey set header request id key, minimum length of 4
func WithHeaderRequestIDKey(key string) RequestIDOption {
	return func(o *requestIDOptions) {
		if len(key) < 4 {
			return
		}
		o.headerXRequestIDKey = key
	}
}

// WithSnow 设置雪花算法ID生成器
func WithSnow(snow *snowflake.Node) RequestIDOption {
	return func(o *requestIDOptions) {
		o.snow = snow
	}
}

// CtxKeyString for context.WithValue key type
type CtxKeyString string

// RequestIDKey request_id
var RequestIDKey = CtxKeyString(string(logger.ContextKeyRequestID))

// -------------------------------------------------------------------------------------------

// RequestID is an interceptor that injects a 'request id' into the context and request/response header of each request.
func RequestID(opts ...RequestIDOption) gin.HandlerFunc {
	// customized request id key
	o := defaultRequestIDOptions()
	o.apply(opts...)
	o.setRequestIDKey()

	return func(c *gin.Context) {
		// Check for incoming header, use it if exists
		//requestID := c.Request.Header.Get(HeaderXRequestIDKey)
		//
		//// Create request id
		//if requestID == "" {
		//	requestID = krand.String(krand.R_All, 10)
		//	c.Request.Header.Set(HeaderXRequestIDKey, requestID)
		//}

		requestID := ""
		if o.snow != nil {
			requestID = o.snow.Generate().String()
		} else {
			requestID = krand.String(krand.R_All, 32)
		}

		// Expose it for use in the application
		c.Set(string(logger.ContextKeyRequestID), requestID)

		// Set X-Request-Id header
		c.Writer.Header().Set(HeaderXRequestIDKey, requestID)

		// 使用 logger 包统一的 context key 类型，确保 logger 能正确提取 request_id
		ctx := context.WithValue(c.Request.Context(), logger.ContextKeyForRequestID(), requestID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// GCtxRequestID get request id from gin.Context
func GCtxRequestID(c *gin.Context) string {
	if v, isExist := c.Get(string(logger.ContextKeyRequestID)); isExist {
		if requestID, ok := v.(string); ok {
			return requestID
		}
	}
	return ""
}

// GCtxRequestIDField get request id field from gin.Context
func GCtxRequestIDField(c *gin.Context) zap.Field {
	return zap.String(string(logger.ContextKeyRequestID), GCtxRequestID(c))
}

// HeaderRequestID get request id from the header
func HeaderRequestID(c *gin.Context) string {
	return c.Request.Header.Get(HeaderXRequestIDKey)
}

// HeaderRequestIDField get request id field from header
func HeaderRequestIDField(c *gin.Context) zap.Field {
	return zap.String(HeaderXRequestIDKey, HeaderRequestID(c))
}

// -------------------------------------------------------------------------------------------

// RequestHeaderKey request header key
var RequestHeaderKey = "request_header_key"

// WrapCtx wrap context, put the Keys and Header of gin.Context into context
func WrapCtx(c *gin.Context) context.Context {
	ctx := context.WithValue(c.Request.Context(), logger.ContextKeyForRequestID(), c.GetString(string(logger.ContextKeyRequestID))) //nolint
	return context.WithValue(ctx, RequestHeaderKey, c.Request.Header)                                                               //nolint
}

// AdaptCtx adapt context, if ctx is gin.Context, return gin.Context and context of the transformation
func AdaptCtx(ctx context.Context) (*gin.Context, context.Context) {
	c, ok := ctx.(*gin.Context)
	if ok {
		ctx = WrapCtx(c)
	}
	return c, ctx
}

// GetFromCtx get value from context
func GetFromCtx(ctx context.Context, key string) interface{} {
	return ctx.Value(key)
}

// CtxRequestID get request id from context.Context
func CtxRequestID(ctx context.Context) string {
	v := ctx.Value(logger.ContextKeyRequestID)
	if str, ok := v.(string); ok {
		return str
	}
	return ""
}

// CtxRequestIDField get request id field from context.Context
func CtxRequestIDField(ctx context.Context) zap.Field {
	return zap.String(string(logger.ContextKeyRequestID), CtxRequestID(ctx))
}

// GetFromHeader get value from header
func GetFromHeader(ctx context.Context, key string) string {
	header, ok := ctx.Value(RequestHeaderKey).(http.Header)
	if !ok {
		return ""
	}
	return header.Get(key)
}

// GetFromHeaders get values from header
func GetFromHeaders(ctx context.Context, key string) []string {
	header, ok := ctx.Value(RequestHeaderKey).(http.Header)
	if !ok {
		return []string{}
	}
	return header.Values(key)
}
