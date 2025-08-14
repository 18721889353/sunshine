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
	"reflect"
)

var resourceName = "default"

// SentinelOptions set the gin logger options.
type SentinelOptions func(*sentinelOptions)

func defaultSentinelOptions() *sentinelOptions {
	defaultLogger, _ := zap.NewProduction()
	return &sentinelOptions{
		resourceExtractor: defaultResourceExtractor,
		log:               defaultLogger,
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
	threshold         float64                   // 每秒请求次数（QPS）
	statIntervalInMs  uint32                    // 统计周期1秒
	log               *zap.Logger
	rules             []*flow.Rule
}

// 默认资源提取函数
func defaultResourceExtractor(ctx *gin.Context) string {
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

// WithSentinelLog set log
func WithSentinelLog(log *zap.Logger) SentinelOptions {
	return func(o *sentinelOptions) {
		if log != nil {
			o.log = log
		}
	}
}

// ... existing code ...

// WithSentinelRules set rules
func WithSentinelRules(ruleInfos any) SentinelOptions {
	return func(o *sentinelOptions) {
		var rules []*flow.Rule
		// 使用反射处理slice
		rv := reflect.ValueOf(ruleInfos)
		if rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				item := rv.Index(i)
				// 如果是指针，需要解引用
				if item.Kind() == reflect.Ptr {
					item = item.Elem()
				}

				// 通过反射获取字段值
				resource := item.FieldByName("Resource").String()
				statIntervalInMs := int(item.FieldByName("StatIntervalInMs").Int())
				threshold := item.FieldByName("Threshold").Float()

				rules = append(rules, &flow.Rule{
					Resource:               resource,
					TokenCalculateStrategy: flow.Direct,
					ControlBehavior:        flow.Reject,
					Threshold:              threshold,
					StatIntervalInMs:       uint32(statIntervalInMs),
				})
			}
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
		o.log.Panic("初始化sentinel失败", logger.Err(err))
	}
	_, err = flow.LoadRules(o.rules)
	if err != nil {
		o.log.Panic("初始化sentinel加载限流规则失败", logger.Err(err))
	}
	return SentinelGin.SentinelMiddleware(
		SentinelGin.WithResourceExtractor(o.resourceExtractor),
		SentinelGin.WithBlockFallback(func(ctx *gin.Context) {
			//response.Output(c, http.StatusTooManyRequests, "限流了")
			response.Out(ctx, errcode.TooManyRequests.WithDetails("限流了"))
			ctx.Abort()
			return
		}),
	)
}
