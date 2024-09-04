package routers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/18721889353/sunshine/internal/model"
	"google.golang.org/grpc/metadata"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/handlerfunc"
	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/gin/middleware/metrics"
	"github.com/18721889353/sunshine/pkg/gin/prof"
	"github.com/18721889353/sunshine/pkg/gin/swagger"
	"github.com/18721889353/sunshine/pkg/gin/validator"
	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/docs"
	"github.com/18721889353/sunshine/internal/config"
)

type routeFns = []func(r *gin.Engine, groupPathMiddlewares map[string][]gin.HandlerFunc, singlePathMiddlewares map[string][]gin.HandlerFunc)

var (
	// all route functions
	allRouteFns = make(routeFns, 0)
	// all middleware functions
	allMiddlewareFns = []func(c *middlewareConfig){}
)

// NewRouter_pbExample create a new router
func NewRouter_pbExample() *gin.Engine { //nolint
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(middleware.Cors())

	if config.Get().HTTP.Timeout > 0 {
		// if you need more fine-grained control over your routes, set the timeout in your routes, unsetting the timeout globally here.
		r.Use(middleware.Timeout(time.Second * time.Duration(config.Get().HTTP.Timeout)))
	}

	// access path /apis/swagger/index.html
	swagger.CustomRouter(r, "apis", docs.ApiDocs)

	// request id middleware
	r.Use(middleware.RequestID(middleware.WithSnow(model.GetSnowNode())))

	// logger middleware, to print simple messages, replace middleware.Logging with middleware.SimpleLog
	r.Use(middleware.Logging(
		middleware.WithLog(logger.Get()),
		middleware.WithMaxLen(config.Get().Logger.MaxLen),
		middleware.WithRequestIDFromContext(),
		middleware.WithLogFrom(config.Get().App.Name),
		middleware.WithIgnoreRoutes("/metrics"), // ignore path
	))
	// 将签名添加为全局中间件
	if config.Get().App.OpenSign {
		r.Use(middleware.VerifySignatureMiddleware(config.Get().Sign.SignKey))
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
		r.Use(middleware.RateLimit())
	}

	// circuit breaker middleware
	if config.Get().App.EnableCircuitBreaker {
		r.Use(middleware.CircuitBreaker(
			// set http code for circuit breaker, default already includes 500 and 503
			middleware.WithValidCode(errcode.InternalServerError.Code()),
			middleware.WithValidCode(errcode.ServiceUnavailable.Code()),
		))
	}

	// trace middleware
	if config.Get().App.EnableTrace {
		r.Use(middleware.Tracing(config.Get().App.Name))
	}

	// profile performance analysis
	if config.Get().App.EnableHTTPProfile {
		prof.Register(r, prof.WithIOWaitTime())
	}

	// validator
	binding.Validator = validator.Init()

	r.GET("/health", handlerfunc.CheckHealth)
	r.GET("/ping", handlerfunc.Ping)
	r.GET("/codes", handlerfunc.ListCodes)
	r.GET("/config", gin.WrapF(errcode.ShowConfig([]byte(config.Show()))))

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

type middlewareConfig struct {
	groupPathMiddlewares  map[string][]gin.HandlerFunc // middleware functions corresponding to route group
	singlePathMiddlewares map[string][]gin.HandlerFunc // middleware functions corresponding to a single route
}

func newMiddlewareConfig() *middlewareConfig {
	return &middlewareConfig{
		groupPathMiddlewares:  make(map[string][]gin.HandlerFunc),
		singlePathMiddlewares: make(map[string][]gin.HandlerFunc),
	}
}

func (c *middlewareConfig) setGroupPath(groupPath string, handlers ...gin.HandlerFunc) { //nolint
	if groupPath == "" {
		return
	}
	if groupPath[0] != '/' {
		groupPath = "/" + groupPath
	}

	handlerFns, ok := c.groupPathMiddlewares[groupPath]
	if !ok {
		c.groupPathMiddlewares[groupPath] = handlers
		return
	}

	c.groupPathMiddlewares[groupPath] = append(handlerFns, handlers...)
}

func (c *middlewareConfig) setSinglePath(method string, singlePath string, handlers ...gin.HandlerFunc) { //nolint
	if method == "" || singlePath == "" {
		return
	}

	key := getSinglePathKey(method, singlePath)
	handlerFns, ok := c.singlePathMiddlewares[key]
	if !ok {
		c.singlePathMiddlewares[key] = handlers
		return
	}

	c.singlePathMiddlewares[key] = append(handlerFns, handlers...)
}

func getSinglePathKey(method string, singlePath string) string { //nolint
	return strings.ToUpper(method) + "->" + singlePath
}

// 自定义ctx，主要是从gin中获取需要的信息，通过ctx传递

func MyCtx(c *gin.Context) context.Context {
	// 在这里获取client ip
	clientIP := c.ClientIP()
	ctx := middleware.WrapCtx(c)
	//创建一个新的传出上下文
	md := metadata.New(map[string]string{
		"clientIP": clientIP,
		// set metadata to be passed from http to rpc
		middleware.ContextRequestIDKey:    middleware.GCtxRequestID(c),                    // request_id
		middleware.HeaderAuthorizationKey: c.GetHeader(middleware.HeaderAuthorizationKey), // authorization
	})
	return metadata.NewOutgoingContext(ctx, md)
	//ctx = metadata.NewIncomingContext(ctx, md)
	////return ctx
	//ctx = context.WithValue(ctx, "clientIP", clientIP)
	//ctx = context.WithValue(ctx, middleware.ContextRequestIDKey, c.GetString(middleware.ContextRequestIDKey))
	//dump.P(metautils.ExtractOutgoing(ctx).Get(middleware.HeaderAuthorizationKey), ctx.Value("clientIP"), ctx.Value(middleware.ContextRequestIDKey), ctx.Value(middleware.HeaderAuthorizationKey))
	//// 赋值到ctx
	//return context.WithValue(ctx, middleware.HeaderAuthorizationKey, c.GetHeader(middleware.HeaderAuthorizationKey))
}
