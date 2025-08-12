package middleware

import (
	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/logger"
	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/flow"
	SentinelGin "github.com/alibaba/sentinel-golang/pkg/adapters/gin"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SentinelOptions set the gin logger options.
type SentinelOptions func(*sentinelOptions)

func defaultSentinelOptions() *sentinelOptions {
	defaultLogger, _ = zap.NewProduction()
	return &sentinelOptions{
		resourceName:     "default",
		threshold:        10,   //每秒请求次数
		statIntervalInMs: 1000, //毫秒1秒
		log:              defaultLogger,
	}
}

type sentinelOptions struct {
	resourceName     string  // 限流资源名（如 "/api/v1/users"）
	threshold        float64 // 每秒请求次数（QPS）
	statIntervalInMs uint32  // 统计周期1秒
	log              *zap.Logger
}

// WithSentinelResourceName set log
func WithSentinelResourceName(resourceName string) SentinelOptions {
	return func(o *sentinelOptions) {
		o.resourceName = resourceName
	}
}

// WithSentinelStatIntervalInMs set log
func WithSentinelStatIntervalInMs(statIntervalInMs uint32) SentinelOptions {
	return func(o *sentinelOptions) {
		o.statIntervalInMs = statIntervalInMs
	}
}

// WithSentinelThreshold set log
func WithSentinelThreshold(threshold float64) SentinelOptions {
	return func(o *sentinelOptions) {
		o.threshold = threshold
	}
}

// WithSentinelLog set log
func WithSentinelLog(log *zap.Logger) Option {
	return func(o *options) {
		if log != nil {
			o.log = log
		}
	}
}
func (o *sentinelOptions) apply(opts ...SentinelOptions) {
	for _, opt := range opts {
		opt(o)
	}
}

// SentinelMiddleware 返回一个 Gin 中间件，用于限流控制
func SentinelMiddleware(opts ...SentinelOptions) gin.HandlerFunc {
	o := defaultSentinelOptions()
	o.apply(opts...)
	//初始化sentinel
	err := sentinel.InitDefault()
	if err != nil {
		o.log.Panic("初始化sentinel失败", logger.Err(err))
	}
	_, err = flow.LoadRules([]*flow.Rule{
		{
			Resource:               o.resourceName,     // 默认资源名
			TokenCalculateStrategy: flow.Direct,        // 直接使用阈值
			ControlBehavior:        flow.Reject,        // 超过阈值直接拒绝
			Threshold:              o.threshold,        // 100 QPS
			StatIntervalInMs:       o.statIntervalInMs, // 统计周期1秒
		},
	})
	if err != nil {
		o.log.Panic("初始化sentinel加载限流规则失败", logger.Err(err))
	}
	return SentinelGin.SentinelMiddleware(
		SentinelGin.WithResourceExtractor(func(ctx *gin.Context) string {
			return o.resourceName
		}),
		SentinelGin.WithBlockFallback(func(ctx *gin.Context) {
			//response.Output(c, http.StatusTooManyRequests, "限流了")
			response.Out(ctx, errcode.TooManyRequests.WithDetails("限流了"))
			ctx.Abort()
			return
		}),
	)
}
