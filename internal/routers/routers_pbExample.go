package routers

import (
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/database"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/handlerfunc"
	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/gin/middleware/metrics"
	"github.com/18721889353/sunshine/pkg/gin/swagger"
	"github.com/18721889353/sunshine/pkg/gin/validator"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"

	"github.com/18721889353/sunshine/docs"
	"github.com/18721889353/sunshine/internal/config"
)

var (
	// all middleware functions
	allMiddlewareFns []func(c *middlewareConfig)

	// all route functions
	allRouteFns = make([]func(r *gin.Engine, groupPathMiddlewares map[string][]gin.HandlerFunc, singlePathMiddlewares map[string][]gin.HandlerFunc), 0)
)

//func customLogFunc(c *gin.Context, reqBody []byte, respBody []byte, startTime time.Time, endTime time.Time, spendTime int64) {
//	go func() {
//		// 保存到数据库
//		result := database.GetDB().WithContext(context.WithoutCancel(c.Request.Context())).Create(&model.CpDealerApiLog{
//			Type:       "接口",
//			Category:   "API",
//			IP:         c.ClientIP(),
//			Url:        c.Request.URL.String(),
//			Params:     string(reqBody),
//			Response:   string(respBody),
//			StartTime:  cast.ToString(startTime.UnixMilli()),
//			EndTime:    cast.ToString(endTime.UnixMilli()),
//			SpendTime:  cast.ToInt(spendTime),
//			DealerID:   cast.ToInt(c.GetString("uid")),
//			Active:     lo.ToPtr("golang api"),
//			CreateTime: cast.ToString(time.Now().Unix()),
//			UpdateTime: int(time.Now().Unix()),
//		})
//		if result.Error != nil {
//			// 打印错误，至少知道为什么丢了
//			logger.ErrorWithCtx(c.Request.Context(), "cp_dealer_api_log insert failed",
//				logger.String("error", result.Error.Error()))
//		}
//	}()
//}

// NewRouter_pbExample 创建并返回一个支持 PB 路由注册的 gin.Engine 实例。
// 与 NewRouter 的区别在于使用自定义 swagger 路径（/apis/swagger/index.html），
// 并通过 allRouteFns / allMiddlewareFns 动态注册路由和中间件。
//
// 返回值:
//   - *gin.Engine: 配置完成的路由引擎实例。
func NewRouter_pbExample() *gin.Engine { //nolint
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(middleware.Cors())
	cfg := config.Get()

	if cfg.HTTP.Timeout > 0 {
		// if you need more fine-grained control over your routes, set the timeout in your routes, unsetting the timeout globally here.
		r.Use(middleware.Timeout(time.Second * time.Duration(cfg.HTTP.Timeout)))
	}

	if cfg.App.Env != "prod" {
		r.GET("/config", gin.WrapF(errcode.ShowConfig([]byte(config.Show()))))
		// access path /apis/swagger/index.html
		swagger.CustomRouter(r, "apis", docs.ApiDocs)
	}
	// validator
	binding.Validator = validator.Init()

	r.GET("/health", handlerfunc.CheckHealth)
	r.GET("/ping", handlerfunc.Ping)
	r.GET("/codes", handlerfunc.ListCodes)

	// request id middleware
	r.Use(middleware.RequestID(middleware.WithSnow(database.GetSnowNode())))

	// trace middleware（必须在 Logging 之前注册，这样 Logging 才能获取到 trace_id）
	if cfg.App.EnableTrace {
		r.Use(middleware.Tracing(cfg.App.Name))
		//r.Use(otelgin.Middleware(cfg.App.Name))
	}

	// logger middleware, to print simple messages, replace middleware.Logging with middleware.SimpleLog
	r.Use(middleware.Logging(
		middleware.WithMaxLen(cfg.Logger.MaxLen),
		middleware.WithLogFrom(cfg.App.Name+"_"+utils.GetLocalIP()),
		middleware.WithIgnoreRoutes("/metrics"), // ignore path
	))

	// APILogMiddleware 必须在 RequestID/Tracing/Logging 之后注册
	// 这样 customLogFunc 捕获的 context 才包含 request_id、trace_id、caller_func
	//r.Use(middleware.APILogMiddleware(middleware.WithAPILogFunc(customLogFunc)))

	// 将签名添加为全局中间件
	if cfg.App.OpenSign {
		r.Use(
			middleware.VerifySignatureMiddleware(
				middleware.WithSignKey(cfg.Sign.SignKey),
				middleware.WithIgnoreURL(cfg.Sign.IgnoreUrls.HTTP...),
				middleware.WithSignExpiredTime(time.Duration(cfg.Sign.SignExpiredTime)*time.Second),
			),
		)
	}
	// 将XSSMiddleware添加为全局中间件
	if cfg.App.OpenXSS {
		r.Use(middleware.XSSCrossMiddleware())
	}
	// metrics middleware
	if cfg.App.EnableMetrics {
		r.Use(metrics.Metrics(r,
			//metrics.WithMetricsPath("/metrics"),                // default is /metrics
			metrics.WithIgnoreStatusCodes(http.StatusNotFound), // ignore 404 status codes
		))
	}

	// Sentinel combined middleware: rate limiting + circuit breaker
	// IMPORTANT: Both SentinelGin.SentinelMiddleware and CircuitBreaker call sentinel.Entry()
	// internally. Using them as separate middlewares would create 2 entries per request,
	// doubling all traffic statistics and making rate limiting effectively 2x stricter.
	// Therefore, we always pass both flow and breaker rules to a single SentinelMiddleware.
	if cfg.App.EnableLimit || cfg.App.EnableCircuitBreaker {
		var sentinelRules []*flow.Rule
		if cfg.App.EnableLimit {
			for _, r := range cfg.Sentinel.LimitRules {
				sentinelRules = append(sentinelRules, &flow.Rule{
					Resource:               r.Resource,
					TokenCalculateStrategy: middleware.ParseTokenCalculateStrategy(r.TokenCalculateStrategy),
					ControlBehavior:        middleware.ParseControlBehavior(r.ControlBehavior),
					Threshold:              r.Threshold,
					StatIntervalInMs:       uint32(r.StatIntervalInMs),
				})
			}
		}

		var breakerRules []*circuitbreaker.Rule
		if cfg.App.EnableCircuitBreaker {
			for _, r := range cfg.Sentinel.BreakerRules {
				breakerRules = append(breakerRules, &circuitbreaker.Rule{
					Resource:         r.Resource,
					Strategy:         middleware.ParseBreakerStrategy(r.Strategy),
					RetryTimeoutMs:   uint32(r.RetryTimeoutMs),
					MinRequestAmount: uint64(r.MinRequestAmount),
					StatIntervalMs:   uint32(r.StatIntervalMs),
					Threshold:        r.Threshold,
				})
			}
		}

		// 需要构建一个 opts 切片，避免 WithSentinelFlowRules 覆盖默认值
		// 当 EnableLimit=false 时，sentinelRules 为空，WithSentinelFlowRules 不会覆盖
		// 同理 EnableCircuitBreaker=false 时，WithSentinelCircuitBreakerRules 也不会覆盖
		r.Use(
			middleware.SentinelMiddleware(
				middleware.WithSentinelResourceExtractor(func(c *gin.Context) string {
					return c.FullPath()
				}),
				middleware.WithSentinelFlowRules(sentinelRules),
				middleware.WithSentinelCircuitBreakerRules(breakerRules),
			),
		)
	}

	if cfg.App.OpenJwt {
		//全局权限验证
		r.Use(
			middleware.Auth(
				middleware.WithSwitchHTTPCode(),
				middleware.WithJwtIgnoreMethods(cfg.Jwt.IgnoreMethods.HTTP...)),
		)
	}

	c := newMiddlewareConfig()

	// set up all middlewares
	for _, fn := range allMiddlewareFns {
		fn(c)
	}

	// register all routes
	for _, fn := range allRouteFns {
		fn(r, c.groupPathMiddlewares, c.singlePathMiddlewares)
	}

	return r
}

// middlewareConfig 存储 PB 路由的中间件配置，按路由分组和单一路径分别管理。
type middlewareConfig struct {
	groupPathMiddlewares  map[string][]gin.HandlerFunc // middleware functions corresponding to route group
	singlePathMiddlewares map[string][]gin.HandlerFunc // middleware functions corresponding to a single route
}

// newMiddlewareConfig 创建并初始化 middlewareConfig 实例。
//
// 返回值:
//   - *middlewareConfig: 初始化完成的路由中间件配置实例。
func newMiddlewareConfig() *middlewareConfig {
	return &middlewareConfig{
		groupPathMiddlewares:  make(map[string][]gin.HandlerFunc),
		singlePathMiddlewares: make(map[string][]gin.HandlerFunc),
	}
}

// setGroupPath 为指定的路由分组添加一组中间件处理函数。
// 如果多次调用同一 groupPath，中间件会以追加方式累积（通常用于不同模块叠加功能）。
// 注意：本函数不负责去重，也不处理中间件顺序冲突，调用方需自行保证逻辑正确性。
func (c *middlewareConfig) setGroupPath(groupPath string, handlers ...gin.HandlerFunc) { //nolint
	// 1. 空路径或空处理程序直接返回，避免无效存储
	if groupPath == "" || len(handlers) == 0 {
		return
	}
	// 2. 规范化路径：
	//    - 使用 path.Clean 去除多余的斜杠和相对路径（如 /api/../v1 -> /v1）
	//    - 确保以 / 开头，否则补全
	cleaned := path.Clean(groupPath)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	// 3. 去除尾部斜杠（与 Gin 的路由分组行为保持一致，通常分组路径不带尾部斜杠）
	cleaned = strings.TrimSuffix(cleaned, "/")
	if cleaned == "" {
		cleaned = "/"
	}
	// 4. 存储或追加中间件
	existing, exists := c.groupPathMiddlewares[cleaned]
	if !exists {
		c.groupPathMiddlewares[cleaned] = handlers
	} else {
		// 追加新的处理程序（如需覆盖，可在此修改逻辑）
		c.groupPathMiddlewares[cleaned] = append(existing, handlers...)
	}
}

// setSinglePath 为指定的单个路由（HTTP 方法 + 路径）添加一组中间件处理函数。
// 多次调用同一 (method, singlePath) 时，中间件以追加方式累积。
// 注意：本函数不处理中间件去重或顺序冲突，调用方需自行保证逻辑正确性。
func (c *middlewareConfig) setSinglePath(method string, singlePath string, handlers ...gin.HandlerFunc) { //nolint
	// 1. 校验必要参数：方法、路径、处理程序均不能为空
	if method == "" || singlePath == "" || len(handlers) == 0 {
		return
	}

	// 2. 规范化路径：
	//    - 使用 path.Clean 去除多余的斜杠和相对路径（如 /api/../v1 -> /v1）
	//    - 确保以 / 开头
	//    - 去除尾部斜杠（与 Gin 路由注册行为一致，例如 "/user/" 与 "/user" 视为同一路由）
	cleanedPath := path.Clean(singlePath)
	if !strings.HasPrefix(cleanedPath, "/") {
		cleanedPath = "/" + cleanedPath
	}
	cleanedPath = strings.TrimSuffix(cleanedPath, "/")
	if cleanedPath == "" {
		cleanedPath = "/"
	}

	// 3. 构造唯一键：方法大写 + "->" + 规范化路径
	key := strings.ToUpper(method) + "->" + cleanedPath

	// 4. 存储或追加中间件
	existing, exists := c.singlePathMiddlewares[key]
	if !exists {
		c.singlePathMiddlewares[key] = handlers
	} else {
		// 追加新的处理程序（如需覆盖行为，可在此调整）
		c.singlePathMiddlewares[key] = append(existing, handlers...)
	}
}
