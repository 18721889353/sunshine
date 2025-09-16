package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"

	"github.com/18721889353/sunshine/pkg/app"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

var _ app.IServer = (*rabbitmqConsumerServer)(nil)

type rabbitmqConsumerServer struct {
	wg        sync.WaitGroup
	isRunning bool
	cancel    context.CancelFunc

	instance  *registry.ServiceInstance
	iRegistry registry.Registry
	consumers []mq.Consumer
}

func (s *rabbitmqConsumerServer) IsRunning() bool {
	return s.isRunning
}

// Start 启动RabbitMQ消费者服务
func (s *rabbitmqConsumerServer) Start() error {
	if s.iRegistry != nil {
		ctx, _ := context.WithTimeout(context.Background(), 5*time.Second) //nolint
		if _, err := s.iRegistry.Register(ctx, s.instance); err != nil {
			return err
		}
		go func() {
			ticker := time.NewTicker(15 * time.Second) // 每15秒检查一次
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					ctx, _ := context.WithTimeout(context.Background(), 5*time.Second) //nolint
					if _, err := s.iRegistry.Register(ctx, s.instance); err != nil {
						logger.Warn("s.iRegistry.Register error", logger.Err(err))
					} else {
						logger.Warn("s.iRegistry.Register")
					}
				}
			}
		}()
	}
	if s.isRunning {
		return fmt.Errorf("rabbitmqConsumer server is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	// 启动所有消费者
	for _, consumer := range s.consumers {
		logger.Info("Starting consumer", logger.Any("name", consumer.Name()))
		if err := consumer.Start(); err != nil {
			logger.Error("Failed to start consumer err", logger.Any("body", consumer.Name()), logger.Err(err))
			return fmt.Errorf("failed to start consumer %s: %w", consumer.Name(), err)
		}
	}

	s.isRunning = true
	logger.Info("rabbitmqConsumer server started")

	// 保持服务运行，直到收到停止信号
	<-ctx.Done()

	logger.Warn("rabbitmqConsumer server stopped")
	return nil
}

// Stop 停止RabbitMQ消费者服务
func (s *rabbitmqConsumerServer) Stop() error {
	logger.Warn("收到停止信号开始停止 rabbitmqConsumer server")
	if s.iRegistry != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		go func() {
			_ = s.iRegistry.Deregister(ctx, s.instance)
			cancel()
		}()
		<-ctx.Done()
	}
	if !s.isRunning {
		return fmt.Errorf("rabbitmqConsumer server is not running")
	}
	//停止所有消费者
	for _, consumer := range s.consumers {
		logger.Warn("开始执行停止 consumer", logger.Any("name", consumer.Name()))
		if err := consumer.Stop(); err != nil {
			logger.Warn("consumer.Stop() err", logger.Any("body", consumer.Name()), logger.Err(err))
		}
	}

	if s.cancel != nil {
		s.cancel()
	}

	s.isRunning = false
	logger.Warn("成功停止 rabbitmqConsumer server")
	return nil
}

// String 返回服务描述
func (s *rabbitmqConsumerServer) String() string {
	return "rabbitmqConsumer service"
}

// NewRabbitmqConsumerServer 创建新的RabbitMQ消费者服务
func NewRabbitmqConsumerServer(consumers []mq.Consumer, opts ...RABBITQMCONSUMEROption) app.IServer {
	o := defaultRABBITQMCONSUMEROptions()
	o.apply(opts...)
	return &rabbitmqConsumerServer{
		isRunning: false,
		iRegistry: o.iRegistry,
		instance:  o.instance,
		consumers: consumers,
	}
}
