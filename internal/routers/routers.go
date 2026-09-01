// Package routers is a package dedicated to registering routes, and supports both
// manual route registration and automatic route registration.
package routers

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/18721889353/sunshine/pkg/utils"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/handlerfunc"
	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/gin/middleware/metrics"
	"github.com/18721889353/sunshine/pkg/gin/prof"
	"github.com/18721889353/sunshine/pkg/gin/validator"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"

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

// VerifySSO 单点登录验证函数（基于数据库 md5_token）
//
//	func VerifySSO(claims *jwt.Claims, _ string, c *gin.Context) error {
//		ctx := c.Request.Context()
//		uid := claims.UID
//		if uid == "" {
//			return errors.New("missing user id in token")
//		}
//
//		userDao := dao.NewSysAdminUserDao(
//			database.GetDB(),
//			cache.NewSysAdminUserCache(database.GetCacheType()),
//		)
//		user, err := userDao.GetByID(ctx, cast.ToUint64(uid))
//		if err != nil {
//			return err
//		}
//
//		authHeader := c.GetHeader(middleware.HeaderAuthorizationKey)
//		if len(authHeader) < 7 {
//			return errors.New("invalid authorization header")
//		}
//		currentToken := authHeader[7:]
//		// token 比对
//		if user.Md5Token != currentToken {
//			return errors.New("token已过期")
//		}
//
//		return nil
//	}

// NewRouter 创建并返回一个 gin.Engine 实例，配置完整的中间件链和路由注册。
//
// 包含以下中间件（按注册顺序）：Recovery、CORS、超时、请求ID、链路追踪、日志、
// 签名校验、XSS 防护、Metrics、限流、熔断、JWT 鉴权。
// 同时注册健康检查、性能分析、Swagger 文档等基础路由。
//
// 返回值:
//   - *gin.Engine: 配置完成的路由引擎实例。
func NewRouter() *gin.Engine {
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(middleware.Cors())
	cfg := config.Get()

	if cfg.HTTP.Timeout > 0 {
		// if you need more fine-grained control over your routes, set the timeout in your routes, unsetting the timeout globally here.
		r.Use(middleware.Timeout(time.Second * time.Duration(cfg.HTTP.Timeout)))
	}
	// validator
	binding.Validator = validator.Init()

	r.GET("/health", handlerfunc.CheckHealth)
	r.GET("/ping", handlerfunc.Ping)
	r.GET("/codes", handlerfunc.ListCodes)

	// pprof 性能分析路由
	// 自动启用 IP 白名单鉴权，防止敏感信息泄露；列表为空时默认仅允许内网访问
	if cfg.App.EnableHTTPProfile {
		pprofOpts := []prof.Option{prof.WithIOWaitTime(), prof.WithAuth(pprofIPWhitelist(cfg.App.PprofIPWhiteList))}
		prof.Register(r, pprofOpts...)
	}

	if cfg.App.Env != "prod" {
		r.GET("/config", gin.WrapF(errcode.ShowConfig([]byte(config.Show()))))
		// register swagger routes, generate code via swag init
		docs.SwaggerInfo.BasePath = ""
		// access path /swagger/index.html
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

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
				middleware.WithJwtIgnoreMethods(cfg.Jwt.IgnoreMethods.HTTP...),
				//middleware.WithVerify(VerifySSO),
			),
		)
	}
	//r.Use(middleware.APILogMiddleware(middleware.WithAPILogFunc(customLogFunc)))

	// register routers, middleware support
	registerRouters(r, "/api/v1", apiV1RouterFns)
	// if you have other group routes you can add them here
	// example:
	//    registerRouters(r, "/api/v2", apiV2RouteFns, middleware.Auth())

	return r
}

// registerRouters 在指定分组路径下注册路由函数集合。
//
// 参数:
//   - r: gin 路由引擎实例。
//   - groupPath: 路由分组路径，如 "/api/v1"。
//   - routerFns: 路由注册函数列表，每个函数接收一个 *gin.RouterGroup。
//   - middlewares: 可选，应用于该分组的中间件函数列表。
func registerRouters(r *gin.Engine, groupPath string, routerFns []func(*gin.RouterGroup), middlewares ...gin.HandlerFunc) {
	group := r.Group(groupPath, middlewares...)
	for _, fn := range routerFns {
		fn(group)
	}
}

// pprofIPWhitelist 返回一个 Gin 中间件，仅允许指定 IP/CIDR 列表内的 IP 访问 pprof。
// 支持两种格式：纯 IP（如 127.0.0.1）和 CIDR（如 10.0.0.0/8）。
// 如果传入的列表为空，使用默认内网段。
func pprofIPWhitelist(cidrs []string) gin.HandlerFunc {
	if len(cidrs) == 0 {
		cidrs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128"}
	}
	ipNets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
			ipNets = append(ipNets, ipNet)
		} else if ip := net.ParseIP(cidr); ip != nil {
			// 纯 IP 自动转为 /32（IPv4）或 /128（IPv6）
			mask := net.CIDRMask(32, 32)
			if ip.To4() == nil {
				mask = net.CIDRMask(128, 128)
			}
			ipNets = append(ipNets, &net.IPNet{IP: ip.Mask(mask), Mask: mask})
		}
	}
	return func(c *gin.Context) {
		realIP := c.ClientIP()
		for _, ipNet := range ipNets {
			if ipNet.Contains(net.ParseIP(realIP)) {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("Forbidden: IP %s 不在白名单中", realIP)})
	}
}
