package server

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"

	"github.com/18721889353/sunshine/pkg/app"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

var _ app.IServer = (*rabbitmqConsumerServer)(nil)

type rabbitmqConsumerServer struct {
	isRunning         atomic.Bool               // 运行状态标记，防止重复启动
	instance          *registry.ServiceInstance // 服务注册实例信息
	mqServerCtxCancel context.CancelFunc        // 取消函数，调用后所有消费者感知 ctx.Done() 退出
	iRegistry         registry.Registry         // 服务注册中心
	consumers         []mq.Consumer             // 消费者列表
}

func (s *rabbitmqConsumerServer) IsRunning() bool {
	return s.isRunning.Load()
}

// Start 启动 RabbitMQ 消费者服务，按顺序执行：启动消费者 → 注册服务发现 → 阻塞等待退出信号。
// 消费者启动失败时会立即返回错误，已启动的消费者由调用方负责清理。
//
// 返回值:
//   - error: 服务启动成功返回 nil（阻塞中），启动失败返回具体错误信息
func (s *rabbitmqConsumerServer) Start() error {
	if s.isRunning.Load() {
		return fmt.Errorf("rabbitmqConsumer server is already running")
	}

	// 创建可取消的上下文，用于统一控制所有消费者的生命周期
	ctx, cancel := context.WithCancel(context.Background())
	s.mqServerCtxCancel = cancel

	// 启动所有消费者
	for i, consumer := range s.consumers {
		logger.InfoWithCtx(context.Background(), "Starting consumer", logger.Any("name", consumer.Name()))
		if err := consumer.Start(ctx); err != nil {
			logger.ErrorWithCtx(context.Background(), "Failed to start consumer err", logger.Any("body", consumer.Name()), logger.Err(err))
			// 取消上下文，通知已启动的 goroutine 退出
			s.mqServerCtxCancel()
			// 回滚：等待已启动的消费者完成退出
			for j := 0; j < i; j++ {
				if stopErr := s.consumers[j].Stop(context.Background()); stopErr != nil {
					logger.WarnWithCtx(context.Background(), "Failed to stop consumer on rollback",
						logger.Any("name", s.consumers[j].Name()), logger.Err(stopErr))
				}
			}
			return fmt.Errorf("failed to start consumer %s: %w", consumer.Name(), err)
		}
	}

	// 注册服务发现
	if s.iRegistry != nil {
		regCtx, regCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer regCancel()
		if err := s.iRegistry.Register(regCtx, s.instance); err != nil {
			s.mqServerCtxCancel()
			return err
		}
	}

	s.isRunning.Store(true)
	logger.InfoWithCtx(context.Background(), "rabbitmqConsumer server started")

	// 保持服务运行，直到收到停止信号
	<-ctx.Done()

	logger.WarnWithCtx(context.Background(), "rabbitmqConsumer server stopped")
	return nil
}

// Stop 停止RabbitMQ消费者服务
func (s *rabbitmqConsumerServer) Stop() error {
	logger.WarnWithCtx(context.Background(), "收到停止信号开始停止 rabbitmqConsumer server")

	if !s.isRunning.Load() {
		return fmt.Errorf("rabbitmqConsumer server is not running")
	}

	// 注销服务实例
	if s.iRegistry != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := s.iRegistry.Deregister(ctx, s.instance); err != nil {
			logger.WarnWithCtx(ctx, "注销服务实例失败", logger.Err(err))
		}
		cancel()
	}

	// 掐断信号线！所有下游感知 ctx.Done()
	if s.mqServerCtxCancel != nil {
		s.mqServerCtxCancel()
	}

	// 并发停止所有消费者
	var wg sync.WaitGroup
	for _, consumer := range s.consumers {
		wg.Add(1)
		go func(c mq.Consumer) {
			defer wg.Done()
			logger.WarnWithCtx(context.Background(), "开始执行停止 consumer", logger.Any("name", c.Name()))
			if err := c.Stop(context.Background()); err != nil {
				logger.WarnWithCtx(context.Background(), "consumer.Stop() err", logger.Any("body", c.Name()), logger.Err(err))
			}
		}(consumer)
	}
	// 等待所有消费者处理完成
	wg.Wait()

	s.isRunning.Store(false)
	logger.WarnWithCtx(context.Background(), "成功停止 rabbitmqConsumer server")
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
		iRegistry: o.iRegistry,
		instance:  o.instance,
		consumers: consumers,
	}
}
