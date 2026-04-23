// Package routers is a package dedicated to registering routes, and supports both
// manual route registration and automatic route registration.
package routers

import (
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"net/http"
	"strconv"
	"time"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/handlerfunc"
	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/gin/middleware/metrics"
	"github.com/18721889353/sunshine/pkg/gin/prof"
	"github.com/18721889353/sunshine/pkg/gin/validator"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/18721889353/sunshine/docs"
	"github.com/18721889353/sunshine/internal/config"
)

var (
	apiV1RouterFns []func(group *gin.RouterGroup) // group router functions
	// if you have other group routes you can define them here
	// example:
	//     apiV2RouterFns []func(r *gin.RouterGroup)
)

//func customLogFunc(c *gin.Context, reqBody []byte, respBody []byte, startTime time.Time, endTime time.Time, spendTime int64) {
//	go func() {
//		// 保存到数据库
//		database.GetDB().Create(&model.CpDealerApiLog{
//			Type:       "接口",
//			Category:   "API",
//			IP:         c.ClientIP(),
//			Url:        c.Request.URL.String(),
//			Params:     string(reqBody),
//			Response:   string(respBody),
//			StartTime:  cast.ToString(startTime.UnixMilli()),
//			EndTime:    cast.ToString(endTime.UnixMilli()),
//			SpendTime:  cast.ToString(spendTime),
//			DealerID:   cast.ToInt(c.GetString("uid")),
//			Active:     "golang api",
//			CreateTime: cast.ToString(time.Now().Unix()),
//			UpdateTime: int(time.Now().Unix()),
//		})
//	}()
//}

// NewRouter create a new router
func NewRouter() *gin.Engine {
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(middleware.Cors())

	if config.Get().HTTP.Timeout > 0 {
		// if you need more fine-grained control over your routes, set the timeout in your routes, unsetting the timeout globally here.
		r.Use(middleware.Timeout(time.Second * time.Duration(config.Get().HTTP.Timeout)))
	}
	// validator
	binding.Validator = validator.Init()

	r.GET("/health", handlerfunc.CheckHealth)
	r.GET("/ping", handlerfunc.Ping)
	r.GET("/codes", handlerfunc.ListCodes)

	// profile performance analysis
	if config.Get().App.EnableHTTPProfile {
		prof.Register(r, prof.WithIOWaitTime())
	}

	if config.Get().App.Env != "prod" {
		r.GET("/config", gin.WrapF(errcode.ShowConfig([]byte(config.Show()))))
		// register swagger routes, generate code via swag init
		docs.SwaggerInfo.BasePath = ""
		// access path /swagger/index.html
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	// request id middleware
	r.Use(middleware.RequestID(middleware.WithSnow(database.GetSnowNode())))

	// trace middleware（必须在 Logging 之前注册，这样 Logging 才能获取到 trace_id）
	if config.Get().App.EnableTrace {
		r.Use(middleware.Tracing(config.Get().App.Name))
		//r.Use(otelgin.Middleware(config.Get().App.Name))
	}

	// logger middleware, to print simple messages, replace middleware.Logging with middleware.SimpleLog
	r.Use(middleware.Logging(
		middleware.WithMaxLen(config.Get().Logger.MaxLen),
		middleware.WithLogFrom(config.Get().App.Name+strconv.Itoa(config.Get().App.MachineID)),
		middleware.WithIgnoreRoutes("/metrics"), // ignore path
	))
	// 将签名添加为全局中间件
	if config.Get().App.OpenSign {
		r.Use(
			middleware.VerifySignatureMiddleware(
				middleware.WithSignKey(config.Get().Sign.SignKey),
				middleware.WithIgnoreURL(config.Get().Sign.IgnoreUrls.HTTP...),
				middleware.WithSignExpiredTime(time.Duration(config.Get().Sign.SignExpiredTime)*time.Second),
			),
		)
	}
	// 将XSSMiddleware添加为全局中间件
	if config.Get().App.OpenXSS {
		r.Use(middleware.XSSCrossMiddleware())
	}
	// metrics middleware
	if config.Get().App.EnableMetrics {
		r.Use(metrics.Metrics(r,
			//metrics.WithMetricsPath("/metrics"),                // default is /metrics
			metrics.WithIgnoreStatusCodes(http.StatusNotFound), // ignore 404 status codes
		))
	}

	// limit middleware
	if config.Get().App.EnableLimit {
		r.Use(
			middleware.SentinelMiddleware(
				middleware.WithSentinelResourceExtractor(func(c *gin.Context) string {
					return c.FullPath()
				}),
				middleware.WithSentinelRules(config.Get().Sentinel.Rules),
			),
		)
		//r.Use(middleware.RateLimit())
	}

	// circuit breaker middleware
	if config.Get().App.EnableCircuitBreaker {
		r.Use(middleware.CircuitBreaker(
			// set http code for circuit breaker, default already includes 500 and 503
			middleware.WithValidCode(errcode.InternalServerError.Code()),
			middleware.WithValidCode(errcode.ServiceUnavailable.Code()),
		))
	}

	if config.Get().App.OpenJwt {
		//全局权限验证
		r.Use(
			middleware.Auth(
				middleware.WithSwitchHTTPCode(),
				middleware.WithJwtIgnoreMethods(config.Get().Jwt.IgnoreMethods.HTTP...)),
		)
	}
	//r.Use(middleware.APILogMiddleware(middleware.WithApiLogFunc(customLogFunc)))

	// register routers, middleware support
	registerRouters(r, "/api/v1", apiV1RouterFns)
	// if you have other group routes you can add them here
	// example:
	//    registerRouters(r, "/api/v2", apiV2RouteFns, middleware.Auth())

	return r
}

func registerRouters(r *gin.Engine, groupPath string, routerFns []func(*gin.RouterGroup), middlewares ...gin.HandlerFunc) {
	group := r.Group(groupPath, middlewares...)
	for _, fn := range routerFns {
		fn(group)
	}
}
