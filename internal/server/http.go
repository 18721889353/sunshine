package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/gin-gonic/gin"

	"github.com/18721889353/sunshine/pkg/app"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"

	"github.com/18721889353/sunshine/internal/routers"
)

var _ app.IServer = (*httpServer)(nil)

type httpServer struct {
	addr   string
	server *http.Server

	instance  *registry.ServiceInstance
	iRegistry registry.Registry
}

// Start http service
func (s *httpServer) Start() error {
	if s.iRegistry != nil {
		ctx, _ := context.WithTimeout(context.Background(), 5*time.Second) //nolint
		if err := s.iRegistry.Register(ctx, s.instance); err != nil {
			return err
		}
	}

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen server error: %v", err)
	}
	return nil
}

// Stop http service
func (s *httpServer) Stop() error {
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

	// 尝试单个关闭策略
	tryStrategy := func(name string, timeout time.Duration, action func(context.Context) error) error {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		logger.InfoWithCtx(context.Background(), fmt.Sprintf("尝试%s，超时时间: %v\n", name, timeout))
		return action(ctx)
	}

	// 分级降级关闭策略
	strategies := []struct {
		name    string
		timeout time.Duration
		action  func(context.Context) error
	}{
		{"快速优雅关闭", 15 * time.Second, func(ctx context.Context) error { return s.server.Shutdown(ctx) }},
		{"标准优雅关闭", 30 * time.Second, func(ctx context.Context) error { return s.server.Shutdown(ctx) }},
		{"最长优雅关闭", 60 * time.Second, func(ctx context.Context) error { return s.server.Shutdown(ctx) }},
		{"最长优雅关闭", 120 * time.Second, func(ctx context.Context) error { return s.server.Shutdown(ctx) }},
		{"强制关闭", 1 * time.Second, func(_ context.Context) error { return s.server.Close() }},
	}

	for _, strategy := range strategies {
		err := tryStrategy(strategy.name, strategy.timeout, strategy.action)
		if err == nil {
			logger.InfoWithCtx(context.Background(), fmt.Sprintf("%s成功\n", strategy.name))
			return nil
		}
		if err == context.DeadlineExceeded {
			logger.InfoWithCtx(context.Background(), fmt.Sprintf("%s超时，尝试下一策略\n", strategy.name))
			continue
		}
		logger.WarnWithCtx(context.Background(), fmt.Sprintf("%s出错: %v\n", strategy.name, err))
		continue
	}
	logger.InfoWithCtx(context.Background(), "所有关闭策略均已尝试，服务关闭完成")
	return nil
}

// String comment
func (s *httpServer) String() string {
	return "http service address " + s.addr
}

// NewHTTPServer 创建并返回一个 HTTP 服务实例，整合了路由引擎、超时配置和服务注册。
// 内部创建 Gin 路由引擎并与 http.Server 绑定，同时携带服务注册所需的实例信息。
// 注意：
//   - addr 格式应为 ":port"（如 ":8080"），由调用方保证格式正确，函数内部不做校验。
//   - 超时参数通过 HTTPOption 传入，零值表示不限制（由 http.Server 默认行为决定）。
//   - 若未提供 WithHTTPRegistry 选项，服务注册功能将被跳过，仅启动纯 HTTP 服务。
func NewHTTPServer(addr string, opts ...HTTPOption) app.IServer {
	// 1. 加载默认配置并应用用户选项
	//    默认配置中 isProd=false、注册相关字段为 nil，
	//    用户传入的 WithHTTP* 选项会覆盖对应字段。
	o := defaultHTTPOptions()
	o.apply(opts...)

	// 2. 根据环境设置 Gin 运行模式
	//    生产环境开启 ReleaseMode 以关闭调试日志和 panic 堆栈，
	//    非生产环境保持 DebugMode 便于开发调试。
	if o.isProd {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	// 3. 创建路由引擎
	router := routers.NewRouter()

	// 4. 构建 http.Server 实例并配置超时参数
	//    readTimeout / writeTimeout: 控制读写操作的超时（0=不限制），防止慢连接耗尽资源
	//    readHeaderTimeout:     请求头读取超时，防止客户端缓慢发送请求头
	//    idleTimeout:          keep-alive 空闲超时（0=不限制），控制长连接复用
	//    MaxHeaderBytes:        限制请求头最大字节数（1MB），防止恶意大请求头攻击
	server := &http.Server{
		Addr:    addr,
		Handler: router,
		ReadTimeout:       o.readTimeout,
		WriteTimeout:      o.writeTimeout,
		ReadHeaderTimeout: o.readHeaderTimeout,
		IdleTimeout:       o.idleTimeout,
		MaxHeaderBytes:    1 << 20,
	}

	// 5. 包装为 httpServer 并返回（含服务注册信息，供 Start 时使用）
	return &httpServer{
		addr:      addr,
		server:    server,
		iRegistry: o.iRegistry,
		instance:  o.instance,
	}
}
