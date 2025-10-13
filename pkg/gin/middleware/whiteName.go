package middleware

import (
	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var defaultWhiteList = map[string]struct{}{}

type WhiteListOption func(*whiteListOptions)

func defaultWhiteListOptions() *whiteListOptions {
	defaultLogger, _ := zap.NewProduction()
	return &whiteListOptions{
		log:       defaultLogger,
		whiteList: defaultWhiteList,
	}
}

type whiteListOptions struct {
	log       *zap.Logger
	whiteList map[string]struct{}
}

func (o *whiteListOptions) apply(opts ...WhiteListOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithWhiteListIP 设置白名单IP
func WithWhiteListIP(ips ...string) WhiteListOption {
	return func(o *whiteListOptions) {
		for _, ip := range ips {
			o.whiteList[ip] = struct{}{}
		}
	}
}

// WithWhiteListLog set log
func WithWhiteListLog(log *zap.Logger) WhiteListOption {
	return func(o *whiteListOptions) {
		if log != nil {
			o.log = log
		}
	}
}

// IPWhiteListMiddleware IP白名单中间件
func IPWhiteListMiddleware(opts ...WhiteListOption) gin.HandlerFunc {
	o := defaultWhiteListOptions()
	o.apply(opts...)

	return func(ctx *gin.Context) {
		// 获取客户端真实IP
		clientIP := ctx.ClientIP()

		// 如果白名单为空，则允许所有IP访问
		if len(o.whiteList) == 0 {
			ctx.Next()
			return
		}

		// 检查IP是否在白名单中
		if _, ok := o.whiteList[clientIP]; !ok {
			o.log.Warn("IP not in whitelist", zap.String("client_ip", clientIP), zap.String("request_path", ctx.Request.URL.Path))
			response.Out(ctx, errcode.Unauthorized.WithDetails("IP address not allowed"))
			ctx.Abort()
			return
		}

		ctx.Next()
	}
}
