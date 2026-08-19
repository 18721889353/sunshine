// Package server is a package that holds the http or grpc service.
//
// 本文件实现了 gRPC 服务端的完整生命周期管理，包括：
// - 服务启动与优雅关闭
// - 一元/流式拦截器链装配（日志、认证、限流、熔断、监控）
// - TLS 安全连接配置
// - pprof 性能分析路由注册
// - 服务注册与发现
// - IP 白名单鉴权中间件
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/utils"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/18721889353/sunshine/pkg/app"
	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/grpc/gtls"
	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
	"github.com/18721889353/sunshine/pkg/grpc/metrics"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/prof"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/service"
)

var _ app.IServer = (*grpcServer)(nil)

var (
	defaultTokenAppID  = "grpc"
	defaultTokenAppKey = "mko09ijn"
)

// grpcServer gRPC 服务端实例，封装了 gRPC 核心组件、HTTP 辅助服务、服务注册等。
type grpcServer struct {
	addr   string
	server *grpc.Server
	listen net.Listener

	// mux 用于承载 pprof 和 metrics 的 HTTP 路由，通过 gRPC 的 HTTP 端口对外暴露。
	mux                             *http.ServeMux
	httpServer                      *http.Server
	registerMetricsMuxAndMethodFunc func() error

	iRegistry registry.Registry
	instance  *registry.ServiceInstance
}

// Start 启动 gRPC 服务。
//
// 1. 如果配置了服务注册，先注册服务实例。
// 2. 如果启用了 metrics，注册 metrics 路由。
// 3. 如果启用了 pprof 或 metrics，启动 HTTP 服务（用于暴露 pprof 和 metrics）。
// 4. 启动 gRPC 服务（阻塞）。
//
// 返回值:
//   - error: 启动过程中的错误。
func (s *grpcServer) Start() error {
	// registration Services
	if s.iRegistry != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.iRegistry.Register(ctx, s.instance); err != nil {
			return err
		}
	}

	if s.registerMetricsMuxAndMethodFunc != nil {
		if err := s.registerMetricsMuxAndMethodFunc(); err != nil {
			return err
		}
	}

	// if either pprof or metrics is enabled, the http service will be started
	if s.mux != nil {
		addr := fmt.Sprintf(":%d", config.Get().Grpc.HTTPPort)
		s.httpServer = &http.Server{
			Addr:        addr,
			Handler:     s.mux,
			IdleTimeout: time.Second * 60,
		}
		go func() {
			logger.InfoWithCtx(context.Background(), "http address of pprof and metrics", logger.String("address", addr))
			if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				panic("listen and serve error: " + err.Error())
			}
		}()
	}

	listen := metrics.NewCustomListener(s.listen, metrics.WithConnectionsLogger(logger.Get()), metrics.WithConnectionsGauge())
	return s.server.Serve(listen)
}

// Stop 优雅关闭 gRPC 服务。
//
// 1. 如果已注册服务，先注销服务实例。
// 2. 优雅停止 gRPC 服务（不再接受新请求，等待正在处理的请求完成）。
// 3. 关闭 HTTP 辅助服务。
//
// 返回值:
//   - error: 关闭过程中的错误。
func (s *grpcServer) Stop() error {
	if s.iRegistry != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		go func() {
			defer cancel()
			if err := s.iRegistry.Deregister(ctx, s.instance); err != nil {
				logger.WarnWithCtx(ctx, "注销服务实例失败", logger.Err(err))
			}
		}()
		<-ctx.Done()
	}

	s.server.GracefulStop()

	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return err
		}
	}

	return nil
}

// String 返回 gRPC 服务地址描述。
//
// 返回值:
//   - string: 服务地址字符串。
func (s *grpcServer) String() string {
	return "grpc service address " + s.addr
}

// secureServerOption 根据配置文件返回 gRPC 安全连接选项。
//
// 支持三种安全模式：
//   - "": 不加密（insecure）
//   - "one-way": 服务端单向认证
//   - "two-way": 客户端和服务端双向 mTLS 认证
//
// 返回值:
//   - grpc.ServerOption: gRPC 安全配置选项，无安全配置时返回 nil。
func (s *grpcServer) secureServerOption() grpc.ServerOption {
	switch config.Get().Grpc.ServerSecure.Type {
	case "one-way":
		credentials, err := gtls.GetServerTLSCredentials(
			config.Get().Grpc.ServerSecure.CertFile,
			config.Get().Grpc.ServerSecure.KeyFile,
		)
		if err != nil {
			panic(err)
		}
		logger.InfoWithCtx(context.Background(), "grpc security type: sever-side certification")
		return grpc.Creds(credentials)

	case "two-way":
		credentials, err := gtls.GetServerTLSCredentialsByCA(
			config.Get().Grpc.ServerSecure.CaFile,
			config.Get().Grpc.ServerSecure.CertFile,
			config.Get().Grpc.ServerSecure.KeyFile,
		)
		if err != nil {
			panic(err)
		}
		logger.InfoWithCtx(context.Background(), "grpc security type: both client-side and server-side certification")
		return grpc.Creds(credentials)
	}

	logger.InfoWithCtx(context.Background(), "grpc security type: insecure")
	return nil
}

// unaryServerOptions 装配一元服务端拦截器链。
//
// 拦截器按以下顺序装配：
//  1. Recovery - 异常恢复
//  2. RequestID - 请求 ID 注入
//  3. Logging - 请求日志
//  4. Token - 简单 Token 认证（可选）
//  5. JWT Auth - JWT 认证（可选）
//  6. Signature - 签名校验（可选）
//  7. Metrics - 指标收集（可选）
//  8. Rate Limit - Sentinel 限流（可选）
//  9. Circuit Breaker - Sentinel 熔断（可选）
//
// 返回值:
//   - grpc.ServerOption: 一元拦截器链选项。
func (s *grpcServer) unaryServerOptions() grpc.ServerOption {
	unaryServerInterceptors := []grpc.UnaryServerInterceptor{
		interceptor.UnaryServerRecovery(),
		interceptor.UnaryServerRequestID(),
	}

	// logger interceptor, to print simple messages, replace interceptor.UnaryServerLog with interceptor.UnaryServerSimpleLog
	unaryServerInterceptors = append(unaryServerInterceptors, interceptor.UnaryServerLog(
		interceptor.WithMaxLen(config.Get().Logger.MaxLen),
		interceptor.WithLogFrom(config.Get().App.Name+"_"+utils.GetLocalIP()),
		interceptor.WithReplaceGRPCLogger(),
		interceptor.WithLogIgnoreMethods("/grpc.health.v1.Health/Check"), // 屏蔽健康检查日志
	))

	// token interceptor
	if config.Get().Grpc.EnableToken {
		checkToken := func(appID string, appKey string) error {
			if appID != defaultTokenAppID || appKey != defaultTokenAppKey {
				return status.Errorf(codes.Unauthenticated, "app id or app key checksum failure")
			}
			return nil
		}
		unaryServerInterceptors = append(unaryServerInterceptors, interceptor.UnaryServerToken(checkToken))
	}
	if config.Get().App.OpenJwt {
		// jwt token interceptor
		unaryServerInterceptors = append(unaryServerInterceptors, interceptor.UnaryServerJwtAuth(
			interceptor.WithAuthIgnoreMethods(config.Get().Jwt.IgnoreMethods.Grpc...),
		))
	}

	if config.Get().App.OpenSign {
		unaryServerInterceptors = append(
			unaryServerInterceptors, interceptor.VerifySignatureInterceptor(
				interceptor.WithSignKey(config.Get().Sign.SignKey),
				interceptor.WithSignIgnoreMethods(config.Get().Sign.IgnoreUrls.Grpc...),
				interceptor.WithSignExpiredTime(time.Duration(config.Get().Sign.SignExpiredTime)*time.Second),
			),
		)
	}

	// metrics interceptor
	if config.Get().App.EnableMetrics {
		unaryServerInterceptors = append(unaryServerInterceptors, interceptor.UnaryServerMetrics())
		s.registerMetricsMuxAndMethodFunc = s.registerMetricsMuxAndMethod()
	}

	// limit interceptor
	if config.Get().App.EnableLimit {
		var flowRules []*flow.Rule
		for _, r := range config.Get().Sentinel.LimitRules {
			flowRules = append(flowRules, &flow.Rule{
				Resource:               r.Resource,
				TokenCalculateStrategy: interceptor.ParseTokenCalculateStrategy(r.TokenCalculateStrategy),
				ControlBehavior:        interceptor.ParseControlBehavior(r.ControlBehavior),
				Threshold:              r.Threshold,
				StatIntervalInMs:       uint32(r.StatIntervalInMs),
			})
		}
		unaryServerInterceptors = append(unaryServerInterceptors, interceptor.UnaryServerRateLimit(
			interceptor.WithSentinelFlowRules(flowRules),
		))
	}

	// circuit breaker interceptor
	if config.Get().App.EnableCircuitBreaker {
		var breakerRules []*circuitbreaker.Rule
		for _, r := range config.Get().Sentinel.BreakerRules {
			breakerRules = append(breakerRules, &circuitbreaker.Rule{
				Resource:         r.Resource,
				Strategy:         interceptor.ParseBreakerStrategy(r.Strategy),
				RetryTimeoutMs:   uint32(r.RetryTimeoutMs),
				MinRequestAmount: uint64(r.MinRequestAmount),
				StatIntervalMs:   uint32(r.StatIntervalMs),
				Threshold:        r.Threshold,
			})
		}
		unaryServerInterceptors = append(unaryServerInterceptors, interceptor.UnaryServerCircuitBreaker(
			interceptor.WithSentinelCircuitBreakerRules(breakerRules),
		))
	}

	return grpc.ChainUnaryInterceptor(unaryServerInterceptors...)
}

// streamServerOptions 装配流式服务端拦截器链。
//
// 拦截器按以下顺序装配：
//  1. Recovery - 异常恢复
//  2. Logging - 请求日志
//  3. Token - 简单 Token 认证（可选）
//  4. Metrics - 指标收集（可选）
//  5. Rate Limit - Sentinel 限流（可选）
//  6. Circuit Breaker - Sentinel 熔断（可选）
//
// 返回值:
//   - grpc.ServerOption: 流式拦截器链选项。
func (s *grpcServer) streamServerOptions() grpc.ServerOption {
	streamServerInterceptors := []grpc.StreamServerInterceptor{
		interceptor.StreamServerRecovery(),
	}

	// logger interceptor, to print simple messages, replace interceptor.StreamServerLog with interceptor.StreamServerSimpleLog
	streamServerInterceptors = append(streamServerInterceptors, interceptor.StreamServerLog(
		interceptor.WithReplaceGRPCLogger(),
	))

	// token interceptor
	if config.Get().Grpc.EnableToken {
		checkToken := func(appID string, appKey string) error {
			if appID != defaultTokenAppID || appKey != defaultTokenAppKey {
				return status.Errorf(codes.Unauthenticated, "app id or app key checksum failure")
			}
			return nil
		}
		streamServerInterceptors = append(streamServerInterceptors, interceptor.StreamServerToken(checkToken))
	}

	// metrics interceptor
	if config.Get().App.EnableMetrics {
		streamServerInterceptors = append(streamServerInterceptors, interceptor.StreamServerMetrics())
	}

	// limit interceptor
	if config.Get().App.EnableLimit {
		var flowRules []*flow.Rule
		for _, r := range config.Get().Sentinel.LimitRules {
			flowRules = append(flowRules, &flow.Rule{
				Resource:               r.Resource,
				TokenCalculateStrategy: interceptor.ParseTokenCalculateStrategy(r.TokenCalculateStrategy),
				ControlBehavior:        interceptor.ParseControlBehavior(r.ControlBehavior),
				Threshold:              r.Threshold,
				StatIntervalInMs:       uint32(r.StatIntervalInMs),
			})
		}
		streamServerInterceptors = append(streamServerInterceptors, interceptor.StreamServerRateLimit(
			interceptor.WithSentinelFlowRules(flowRules),
		))
	}

	// circuit breaker interceptor
	if config.Get().App.EnableCircuitBreaker {
		var breakerRules []*circuitbreaker.Rule
		for _, r := range config.Get().Sentinel.BreakerRules {
			breakerRules = append(breakerRules, &circuitbreaker.Rule{
				Resource:         r.Resource,
				Strategy:         interceptor.ParseBreakerStrategy(r.Strategy),
				RetryTimeoutMs:   uint32(r.RetryTimeoutMs),
				MinRequestAmount: uint64(r.MinRequestAmount),
				StatIntervalMs:   uint32(r.StatIntervalMs),
				Threshold:        r.Threshold,
			})
		}
		streamServerInterceptors = append(streamServerInterceptors, interceptor.StreamServerCircuitBreaker(
			interceptor.WithSentinelCircuitBreakerRules(breakerRules),
		))
	}

	return grpc.ChainStreamInterceptor(streamServerInterceptors...)
}

// getOptions 收集所有 gRPC 服务端选项。
//
// 包含安全连接、链路追踪统计处理器、一元拦截器链、流式拦截器链。
//
// 返回值:
//   - []grpc.ServerOption: gRPC 服务端选项列表。
func (s *grpcServer) getOptions() []grpc.ServerOption {
	var options []grpc.ServerOption

	secureOption := s.secureServerOption()
	if secureOption != nil {
		options = append(options, secureOption)
	}

	// Add StatsHandler for tracing if enabled
	if config.Get().App.EnableTrace {
		statsHandler := interceptor.NewServerStatsHandler()
		options = append(options, grpc.StatsHandler(statsHandler))
	}

	options = append(options, s.unaryServerOptions())
	options = append(options, s.streamServerOptions())

	return options
}

// registerMetricsMuxAndMethod 注册 metrics HTTP 路由并返回注册函数。
//
// 返回值:
//   - func() error: 注册 metrics 路由的闭包函数。
func (s *grpcServer) registerMetricsMuxAndMethod() func() error {
	return func() error {
		if s.mux == nil {
			s.mux = http.NewServeMux()
		}
		metrics.Register(s.mux, s.server)
		return nil
	}
}

// registerProfMux 注册 pprof 性能分析 HTTP 路由。
//
// 生产环境自动启用 IP 白名单鉴权，防止敏感信息泄露；
// dev/test 环境免鉴权方便调试。
func (s *grpcServer) registerProfMux() {
	if s.mux == nil {
		s.mux = http.NewServeMux()
	}

	pprofOpts := []prof.HTTPOption{prof.WithIOWaitTime()}
	if config.Get().App.Env == "prod" {
		pprofOpts = append(pprofOpts, prof.WithAuth(pprofIPWhitelist(config.Get().App.PprofIPWhiteList)))
	}
	prof.Register(s.mux, pprofOpts...)
}

// addHTTPRouter 注册辅助 HTTP 路由（错误码列表和配置展示）。
func (s *grpcServer) addHTTPRouter() {
	if s.mux == nil {
		s.mux = http.NewServeMux()
	}
	s.mux.HandleFunc("/codes", errcode.ListGRPCErrCodes)

	cfgStr := config.Show()
	s.mux.HandleFunc("/config", errcode.ShowConfig([]byte(cfgStr)))
}

// NewGRPCServer 创建并返回一个新的 gRPC 服务端实例。
//
// 初始化流程：
//  1. 注册辅助 HTTP 路由（/codes、/config）。
//  2. 如果启用了 pprof，注册 pprof 路由。
//  3. 监听 TCP 端口。
//  4. 创建 gRPC 服务实例并注册所有服务。
//
// 参数:
//   - addr: 监听地址，如 ":7001"。
//   - opts: 可选，gRPC 服务选项。
//
// 返回值:
//   - app.IServer: gRPC 服务端接口实例。
func NewGRPCServer(addr string, opts ...GrpcOption) app.IServer {
	var err error
	o := defaultGrpcOptions()
	o.apply(opts...)
	s := &grpcServer{
		addr:      addr,
		iRegistry: o.iRegistry,
		instance:  o.instance,
	}
	s.addHTTPRouter()
	if config.Get().App.EnableHTTPProfile {
		s.registerProfMux()
	}

	s.listen, err = net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}

	s.server = grpc.NewServer(s.getOptions()...)
	service.RegisterAllService(s.server)
	return s
}

// pprofIPWhitelist 返回 HTTP 中间件，仅允许指定 IP/CIDR 列表内的 IP 访问 pprof。
//
// 支持两种格式：纯 IP（如 127.0.0.1）和 CIDR（如 10.0.0.0/8）。
// 如果传入的列表为空，使用默认内网段（10.0.0.0/8、172.16.0.0/12、192.168.0.0/16）。
//
// 参数:
//   - cidrs: IP/CIDR 白名单列表。
//
// 返回值:
//   - func(http.Handler) http.Handler: HTTP 中间件处理函数。
func pprofIPWhitelist(cidrs []string) func(http.Handler) http.Handler {
	if len(cidrs) == 0 {
		cidrs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	}
	ipNets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
			ipNets = append(ipNets, ipNet)
		} else if ip := net.ParseIP(cidr); ip != nil {
			mask := net.CIDRMask(32, 32)
			if ip.To4() == nil {
				mask = net.CIDRMask(128, 128)
			}
			ipNets = append(ipNets, &net.IPNet{IP: ip.Mask(mask), Mask: mask})
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 优先从 X-Forwarded-For 和 X-Real-IP 获取真实客户端 IP
			// 兼容网关代理场景（Nginx、Kong、API Gateway 等）
			realIP := r.Header.Get("X-Real-IP")
			if realIP == "" {
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					if i := strings.IndexByte(xff, ','); i > 0 {
						realIP = strings.TrimSpace(xff[:i])
					} else {
						realIP = strings.TrimSpace(xff)
					}
				}
			}
			if realIP == "" {
				realIP = r.RemoteAddr
				if host, _, err := net.SplitHostPort(realIP); err == nil {
					realIP = host
				}
			}
			for _, ipNet := range ipNets {
				if ipNet.Contains(net.ParseIP(realIP)) {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, fmt.Sprintf("Forbidden: IP %s 不在白名单中", realIP), http.StatusForbidden)
		})
	}
}
