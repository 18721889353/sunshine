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

// NewHTTPServer creates a new http server
func NewHTTPServer(addr string, opts ...HTTPOption) app.IServer {
	o := defaultHTTPOptions()
	o.apply(opts...)

	if o.isProd {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	router := routers.NewRouter()
	server := &http.Server{
		Addr:    addr,
		Handler: router,
		//ReadTimeout:    time.Second*30,
		//WriteTimeout:   time.Second*60,
		MaxHeaderBytes: 1 << 20,
	}

	return &httpServer{
		addr:      addr,
		server:    server,
		iRegistry: o.iRegistry,
		instance:  o.instance,
	}
}

// delete the templates code start

// NewHTTPServer_pbExample creates a new web server
func NewHTTPServer_pbExample(addr string, opts ...HTTPOption) app.IServer { //nolint
	o := defaultHTTPOptions()
	o.apply(opts...)

	if o.isProd {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	router := routers.NewRouter_pbExample()
	server := &http.Server{
		Addr:    addr,
		Handler: router,
		//ReadTimeout:    time.Second*30,
		//WriteTimeout:   time.Second*60,
		IdleTimeout:    time.Second * 60, //当 HTTP 连接在 60 秒内没有任何活动时，服务器将主动关闭该连接
		MaxHeaderBytes: 1 << 20,
	}

	return &httpServer{
		addr:      addr,
		server:    server,
		iRegistry: o.iRegistry,
		instance:  o.instance,
	}
}

// delete the templates code end
