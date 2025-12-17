// Package middleware is gin middleware plugin.
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"
	"go.uber.org/zap"
	"time"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/jwt"
)

const (
	// HeaderAuthorizationKey http header authorization key
	HeaderAuthorizationKey = "Authorization"
)

type jwtOptions struct {
	log              *zap.Logger
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
	defaultLogger, _ := zap.NewProduction()
	return &jwtOptions{
		log:              defaultLogger,
		isSwitchHTTPCode: false,
		verify:           nil,
		ignoreMethods:    make(map[string]struct{}), // 忽略的方法
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

// WithAuthLog set log
func WithAuthLog(log *zap.Logger) JwtOption {
	return func(o *jwtOptions) {
		if log != nil {
			o.log = log
		}
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

// WithAuthUidFields 设置用户ID字段名列表，按优先级排序
func WithAuthUidFields(fields ...string) JwtOption {
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

// Auth authorization
func Auth(opts ...JwtOption) gin.HandlerFunc {
	o := defaultJwtOptions()
	o.apply(opts...)
	return func(c *gin.Context) {
		reqID := ""
		fields := []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("method", c.Request.Method),
			zap.String("url", c.Request.URL.String()),
		}
		if v, isExist := c.Get(ContextRequestIDKey); isExist {
			if requestID, ok := v.(string); ok {
				reqID = requestID
				fields = append(fields, zap.String(ContextRequestIDKey, reqID))
			}
		}
		//c.Request.URL.String()
		if _, ok := o.ignoreMethods[c.Request.URL.Path]; ok {
			c.Next()
		} else {
			authorization := c.GetHeader(HeaderAuthorizationKey)
			if len(authorization) < 150 {
				fields = append(fields, zap.String(HeaderAuthorizationKey, authorization))
				o.log.Warn("authorization is illegal", fields...)
				responseUnauthorized(c, o.isSwitchHTTPCode)
				c.Abort()
				return
			}

			token := authorization[7:] // remove Bearer prefix
			claims, err := jwt.ParseToken(token)
			if err != nil {
				fields = append(fields, zap.String("token", token), zap.Error(err))
				o.log.Warn("ParseToken error", fields...)
				responseUnauthorized(c, o.isSwitchHTTPCode)
				c.Abort()
				return
			}

			if o.verify != nil {
				tokenTail10 := token[len(token)-10:]
				if err = o.verify(claims, tokenTail10, c); err != nil {
					fields = append(fields, zap.Error(err), zap.Any("claims", claims), zap.String("uid", claims.UID), zap.String("name", claims.Name))
					o.log.Warn("verify error", fields...)
					responseUnauthorized(c, o.isSwitchHTTPCode)
					c.Abort()
					return
				}
			} else {
				// 优化 UID 设置逻辑，支持更多字段名
				uid := claims.UID
				if uid == "" {
					// 按优先级顺序检查各种可能的 ID 字段
					for _, key := range o.uidFields {
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
				c.Set("uid", uid)
				c.Set("name", claims.Name)
			}
			c.Next()
		}
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
			o.log.Warn("authorization is illegal")
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		token := authorization[7:] // remove Bearer prefix
		claims, err := jwt.ParseCustomToken(token)
		if err != nil {
			o.log.Warn("ParseToken error", zap.Error(err))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		tokenTail10 := token[len(token)-10:]
		if err = verify(claims, tokenTail10, c); err != nil {
			o.log.Warn("verify error", zap.Error(err), zap.Any("fields", claims.Fields))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		c.Next()
	}
}
