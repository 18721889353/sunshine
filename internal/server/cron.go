package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/18721889353/sunshine/internal/database"

	"github.com/18721889353/sunshine/pkg/app"
	"github.com/18721889353/sunshine/pkg/gocron"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

var _ app.IServer = (*cronServer)(nil)

var (
	// globalCronSchedulerOnce 保证仅首个创建的调度器被保存为全局引用（与 HTTP 先例一致）。
	globalCronSchedulerOnce sync.Once
	// globalCronScheduler 保存调度器实例，供配置热更新 openCron 使用。
	globalCronScheduler *gocron.Scheduler
)

type cronServer struct {
	tasks     []*gocron.Task
	scheduler *gocron.Scheduler
	isRunning bool
	cancel    context.CancelFunc

	instance  *registry.ServiceInstance
	iRegistry registry.Registry
}

func (s *cronServer) IsRunning() bool {
	return s.isRunning
}

// Start cron service
func (s *cronServer) Start() error {
	if s.iRegistry != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.iRegistry.Register(ctx, s.instance); err != nil {
			return err
		}
	}
	if s.isRunning {
		return fmt.Errorf("cron server is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	if len(s.tasks) > 0 {
		if err := s.scheduler.Run(s.tasks...); err != nil {
			return fmt.Errorf("failed to run cron tasks: %v", err)
		}
	}

	s.isRunning = true
	logger.InfoWithCtx(context.Background(), "cron server started")

	// 保持服务运行，直到收到停止信号
	<-ctx.Done()

	logger.InfoWithCtx(context.Background(), "cron server stopped")
	return nil
}

// Stop cron service
func (s *cronServer) Stop() error {
	if s.iRegistry != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		go func() {
			defer cancel()
			if err := s.iRegistry.Deregister(ctx, s.instance); err != nil {
				logger.WarnWithCtx(ctx, "注销服务实例失败", logger.Err(err))
			}
		}()
		<-ctx.Done()
	}
	if !s.isRunning {
		return fmt.Errorf("cron server is not running")
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.scheduler.Stop()
	redisCli := database.GetRedisCli()
	for _, task := range s.tasks {
		// 使用SCAN命令完整遍历所有匹配的键
		var cursor uint64
		var keys []string
		var err error

		// 循环扫描直到游标回到0，确保获取所有匹配的键
		for {
			keys, cursor, err = redisCli.Scan(context.Background(), cursor, "*"+task.Name+"LockKey", 100).Result()
			if err != nil {
				logger.ErrorWithCtx(context.Background(), "redisCli.Scan", logger.Err(err))
				break
			}
			// 删除当前批次获取到的键
			for _, key := range keys {
				del := redisCli.Del(context.Background(), key)
				if del.Err() != nil {
					logger.ErrorWithCtx(context.Background(), "redisCli.Del", logger.Err(del.Err()))
				} else {
					logger.InfoWithCtx(context.Background(), "Deleted lock key", logger.String("key", key))
				}
			}
			// 如果游标回到0，表示遍历完成
			if cursor == 0 {
				break
			}
		}
	}
	s.isRunning = false
	logger.InfoWithCtx(context.Background(), "cron server stop signal received")
	return nil
}

// String comment
func (s *cronServer) String() string {
	return "cron service"
}

// GetCronScheduler 获取全局调度器实例（供配置热更新 openCron 使用）。
// 必须在 NewCronServer() 之后才非 nil。
func GetCronScheduler() *gocron.Scheduler {
	return globalCronScheduler
}

// NewCronServer creates a new cron server
func NewCronServer(tasks []*gocron.Task, opts ...CronOption) app.IServer {
	o := defaultCronOptions()
	o.apply(opts...)
	cronSvr := &cronServer{
		tasks:     tasks,
		scheduler: gocron.New(gocron.WithOnlyPrintError(true)),
		isRunning: false,
		iRegistry: o.iRegistry,
		instance:  o.instance,
	}
	// 保存全局引用，供热更新 openCron 使用（首次创建时保存）
	globalCronSchedulerOnce.Do(func() {
		globalCronScheduler = cronSvr.scheduler
	})
	return cronSvr
}
