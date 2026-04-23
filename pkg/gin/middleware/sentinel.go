package middleware

import (
	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/flow"
	SentinelGin "github.com/alibaba/sentinel-golang/pkg/adapters/gin"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/copier"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
)

var resourceName = "default"

// SentinelOptions set the gin logger options.
type SentinelOptions func(*sentinelOptions)

func defaultSentinelOptions() *sentinelOptions {
	return &sentinelOptions{
		resourceExtractor: defaultResourceExtractor,
		rules: []*flow.Rule{
			{
				Resource:               resourceName, // 默认资源名
				TokenCalculateStrategy: flow.Direct,  // 直接使用阈值
				ControlBehavior:        flow.Reject,  // 超过阈值直接拒绝
				Threshold:              100,          // 100 QPS
				StatIntervalInMs:       1000,         // 统计周期1秒
			},
		},
	}
}

type sentinelOptions struct {
	resourceExtractor func(*gin.Context) string // 资源提取器
	rules             []*flow.Rule
}

// 默认资源提取函数
func defaultResourceExtractor(_ *gin.Context) string {
	return resourceName
}

// WithSentinelResourceExtractor 设置资源提取器
func WithSentinelResourceExtractor(fn func(*gin.Context) string) SentinelOptions {
	return func(o *sentinelOptions) {
		if fn != nil {
			o.resourceExtractor = fn
		}
	}
}

// WithSentinelRules set rules
func WithSentinelRules(ruleInfos any) SentinelOptions {
	return func(o *sentinelOptions) {
		var rules []*flow.Rule
		err := copier.Copy(&rules, ruleInfos)
		if err != nil {
			panic("copier.Copy err: " + err.Error())
		}
		o.rules = rules
	}
}

// ... existing code ...

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
		panic("初始化sentinel失败: " + err.Error())
	}
	_, err = flow.LoadRules(o.rules)
	if err != nil {
		panic("初始化sentinel加载限流规则失败: " + err.Error())
	}
	return SentinelGin.SentinelMiddleware(
		SentinelGin.WithResourceExtractor(o.resourceExtractor),
		SentinelGin.WithBlockFallback(func(ctx *gin.Context) {
			//response.Output(c, http.StatusTooManyRequests, "限流了")
			response.Out(ctx, errcode.TooManyRequests.WithDetails("限流了"))
			ctx.Abort()
		}),
	)
}
