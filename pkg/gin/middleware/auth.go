// Package middleware is gin middleware plugin.
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/jwt"
	"github.com/18721889353/sunshine/pkg/logger"
)

const (
	// HeaderAuthorizationKey http header authorization key
	HeaderAuthorizationKey = "Authorization"
)

type jwtOptions struct {
	isSwitchHTTPCode bool
	verify           VerifyFn // verify function, only use in Auth
	ignoreMethods    map[string]struct{}
	uidFields        []string // 用户ID字段名列表，按优先级排序
}

// JwtOption set the jwt options.
type JwtOption func(*jwtOptions)

func (o *jwtOptions) apply(opts ...JwtOption) {
	for _, opt := range opts {
		opt(o)
	}
}

func defaultJwtOptions() *jwtOptions {
	return &jwtOptions{
		isSwitchHTTPCode: false,
		verify:           nil,
		ignoreMethods:    make(map[string]struct{}),                  // 忽略的方法
		uidFields:        []string{"id", "uid", "userId", "user_id"}, // 默认的用户ID字段名列表
	}
}

// WithSwitchHTTPCode switch to http code
func WithSwitchHTTPCode() JwtOption {
	return func(o *jwtOptions) {
		o.isSwitchHTTPCode = true
	}
}

// WithVerify set verify function
func WithVerify(verify VerifyFn) JwtOption {
	return func(o *jwtOptions) {
		o.verify = verify
	}
}

// WithJwtIgnoreMethods 设置忽略jwt的方法
// fullMethodName 格式: /packageName.serviceName/methodName,
// 示例 /api.userExample.v1.userExampleService/GetByID
func WithJwtIgnoreMethods(fullMethodNames ...string) JwtOption {
	return func(o *jwtOptions) {
		for _, method := range fullMethodNames {
			o.ignoreMethods[method] = struct{}{}
		}
	}
}

// WithAuthUIDFields 设置用户ID字段名列表，按优先级排序
func WithAuthUIDFields(fields ...string) JwtOption {
	return func(o *jwtOptions) {
		if len(fields) > 0 {
			o.uidFields = fields
		}
	}
}

func responseUnauthorized(c *gin.Context, isSwitchHTTPCode bool) {
	if isSwitchHTTPCode {
		response.Out(c, errcode.Unauthorized)
	} else {
		response.Error(c, errcode.Unauthorized)
	}
}

// -------------------------------------------------------------------------------------------

// VerifyFn verify function, tokenTail10 is the last 10 characters of the token.
type VerifyFn func(claims *jwt.Claims, tokenTail10 string, c *gin.Context) error

// extractUIDFromClaims 从 claims 中提取 UID
func extractUIDFromClaims(claims *jwt.Claims, uidFields []string) string {
	uid := claims.UID
	if uid == "" {
		// 按优先级顺序检查各种可能的 ID 字段
		for _, key := range uidFields {
			if val, ok := claims.Fields[key]; ok {
				if str, ok := val.(string); ok && str != "" {
					uid = str
					break
				}
				// 如果不是字符串类型，尝试转换为字符串
				if str := cast.ToString(val); str != "" {
					uid = str
					break
				}
			}
		}
	}
	return uid
}

// handleAuthVerification 处理认证验证
func handleAuthVerification(o *jwtOptions, claims *jwt.Claims, token string, c *gin.Context) error {
	if o.verify != nil {
		tokenTail10 := token[len(token)-10:]
		if err := o.verify(claims, tokenTail10, c); err != nil {
			logger.WarnWithCtx(c.Request.Context(), "verify error",
				logger.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
				logger.String("method", c.Request.Method),
				logger.String("url", c.Request.URL.String()),
				logger.Err(err),
				logger.Any("claims", claims),
				logger.String("uid", claims.UID),
				logger.String("name", claims.Name))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return err
		}
	} else {
		// 优化 UID 设置逻辑，支持更多字段名
		uid := extractUIDFromClaims(claims, o.uidFields)
		c.Set("uid", uid)
		c.Set("name", claims.Name)
	}
	return nil
}

// Auth authorization
func Auth(opts ...JwtOption) gin.HandlerFunc {
	o := defaultJwtOptions()
	o.apply(opts...)
	return func(c *gin.Context) {
		if _, ok := o.ignoreMethods[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		authorization := c.GetHeader(HeaderAuthorizationKey)
		if len(authorization) < 150 {
			logger.WarnWithCtx(c.Request.Context(), "authorization is illegal",
				logger.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
				logger.String("method", c.Request.Method),
				logger.String("url", c.Request.URL.String()),
				logger.String(HeaderAuthorizationKey, authorization))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		token := authorization[7:] // remove Bearer prefix
		claims, err := jwt.ParseToken(token)
		if err != nil {
			logger.WarnWithCtx(c.Request.Context(), "ParseToken error",
				logger.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
				logger.String("method", c.Request.Method),
				logger.String("url", c.Request.URL.String()),
				logger.String("token", token),
				logger.Err(err))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		if err := handleAuthVerification(o, claims, token, c); err != nil {
			return
		}

		c.Next()
	}
}

// -------------------------------------------------------------------------------------------

// VerifyCustomFn verify custom function, tokenTail10 is the last 10 characters of the token.
type VerifyCustomFn func(claims *jwt.CustomClaims, tokenTail10 string, c *gin.Context) error

// AuthCustom custom authentication
func AuthCustom(verify VerifyCustomFn, opts ...JwtOption) gin.HandlerFunc {
	o := defaultJwtOptions()
	o.apply(opts...)

	return func(c *gin.Context) {
		authorization := c.GetHeader(HeaderAuthorizationKey)
		if len(authorization) < 150 {
			logger.WarnWithCtx(c.Request.Context(), "authorization is illegal")
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		token := authorization[7:] // remove Bearer prefix
		claims, err := jwt.ParseCustomToken(token)
		if err != nil {
			logger.WarnWithCtx(c.Request.Context(), "ParseToken error", logger.Err(err))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		tokenTail10 := token[len(token)-10:]
		if err = verify(claims, tokenTail10, c); err != nil {
			logger.WarnWithCtx(c.Request.Context(), "verify error", logger.Err(err), logger.Any("fields", claims.Fields))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		c.Next()
	}
}
