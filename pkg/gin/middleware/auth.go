// Package middleware is gin middleware plugin.
package middleware

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"time"

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

		authorization := c.GetHeader(HeaderAuthorizationKey)
		if len(authorization) < 150 {
			fields = append(fields, zap.String(HeaderAuthorizationKey, authorization))
			logger.Warn("authorization is illegal", fields...)
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		token := authorization[7:] // remove Bearer prefix
		claims, err := jwt.ParseToken(token)
		if err != nil {
			fields = append(fields, zap.String("token", token), zap.Error(err))
			logger.Warn("ParseToken error", fields...)
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		if o.verify != nil {
			tokenTail10 := token[len(token)-10:]
			if err = o.verify(claims, tokenTail10, c); err != nil {
				fields = append(fields, zap.Error(err), logger.String("uid", claims.UID), logger.String("name", claims.Name))
				logger.Warn("verify error", fields...)
				responseUnauthorized(c, o.isSwitchHTTPCode)
				c.Abort()
				return
			}
		} else {
			c.Set("uid", claims.UID)
			c.Set("name", claims.Name)
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
			logger.Warn("authorization is illegal")
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		token := authorization[7:] // remove Bearer prefix
		claims, err := jwt.ParseCustomToken(token)
		if err != nil {
			logger.Warn("ParseToken error", logger.Err(err))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		tokenTail10 := token[len(token)-10:]
		if err = verify(claims, tokenTail10, c); err != nil {
			logger.Warn("verify error", logger.Err(err), logger.Any("fields", claims.Fields))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		c.Next()
	}
}
