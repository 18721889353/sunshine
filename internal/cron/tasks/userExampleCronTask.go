// Package tasks 包含定时任务实现
package tasks

//
//import (
//	"context"
//	"fmt"
//	"strings"
//	"time"
//
//	"github.com/18721889353/sunshine/internal/cache"
//	"github.com/18721889353/sunshine/internal/cron"
//	"github.com/18721889353/sunshine/internal/dao"
//	"github.com/18721889353/sunshine/internal/database"
//	"github.com/18721889353/sunshine/pkg/gin/middleware"
//	"github.com/18721889353/sunshine/pkg/gocron"
//	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
//	"github.com/18721889353/sunshine/pkg/logger"
//	"github.com/go-redsync/redsync/v4"
//	"google.golang.org/grpc/metadata"
//)
//
//func init() {
//	cron.RegisterTask(&gocron.Task{
//		Name:      newCronTasksUserExampleService().String(),
//		TimeSpec:  gocron.EveryHour(4),
//		Fn:        newCronTasksUserExampleService().userExampleCronTask,
//		IsRunOnce: false,
//	})
//}
//
//type userExampleCronTaskService struct {
//	isRunning         bool
//	iTkUserExampleDao dao.UserExampleDao
//	cache             cache.UserExampleCache
//}
//
//func newCronTasksUserExampleService() *userExampleCronTaskService {
//	return &userExampleCronTaskService{
//		isRunning: false,
//	}
//}
//func (s *userExampleCronTaskService) String() string {
//	return "userExampleCronTask"
//}
//
//func (s *userExampleCronTaskService) userExampleCronTask() {
//	// 先使用本地互斥锁进行快速检查
//	if s.isRunning {
//		logger.Info(s.String() + "任务已在运行中，跳过本次执行")
//		return
//	}
//	s.isRunning = true
//	// 确保在函数结束时重置运行状态
//	defer func() {
//		s.isRunning = false
//	}()
//	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
//		string(logger.ContextKeyRequestID): database.GetSnowID().String(),
//	}))
//	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
//	defer cancel()
//
//	ctx = timeoutCtx
//	s.iTkUserExampleDao = dao.NewUserExampleDao(
//		database.GetDB(),
//		cache.NewUserExampleCache(database.GetCacheType()),
//	)
//	s.cache = cache.NewUserExampleCache(database.GetCacheType())
//	logger.Info(s.String()+"开始获取分布式锁", interceptor.ServerCtxRequestIDField(ctx))
//	{
//		// 获取分布式锁
//		err := s.cache.GetLock(ctx, s.String()+"LockKey", redsync.WithExpiry(time.Minute*10))
//		if err != nil {
//			if strings.Contains(err.Error(), "lock already taken") {
//				logger.Info(s.String()+"锁已被占用，跳过本次执行", interceptor.ServerCtxRequestIDField(ctx))
//				return
//			} else {
//				logger.Warn(s.String()+"获取分布式锁失败", logger.Err(err), interceptor.ServerCtxRequestIDField(ctx))
//			}
//			// 如果获取锁失败，直接返回，不执行任务
//			return
//		}
//		// 确保在函数结束时释放分布式锁
//		defer func() {
//			if releaseErr := s.cache.ReleaseLock(ctx); releaseErr != nil {
//				logger.Warn(s.String()+"释放分布式锁失败", logger.Err(releaseErr), interceptor.ServerCtxRequestIDField(ctx))
//			} else {
//				logger.Info(s.String()+"分布式锁释放成功", interceptor.ServerCtxRequestIDField(ctx))
//			}
//		}()
//	}
//
//	logger.Info(s.String()+"获取分布式锁成功，开始执行任务", interceptor.ServerCtxRequestIDField(ctx))
//	// 执行实际的处理任务
//
//	if err := s.processUserExamples(ctx); err != nil {
//		logger.Warn(s.String()+"处理失败", logger.Err(err), interceptor.ServerCtxRequestIDField(ctx))
//		return
//	}
//	logger.Info(s.String()+"任务执行完成", interceptor.ServerCtxRequestIDField(ctx))
//}
//
//func (s *userExampleCronTaskService) processUserExamples(ctx context.Context) error {
//	logger.Info(fmt.Sprintf("[%s] 开始执行任务", s.String()), interceptor.ServerCtxRequestIDField(ctx))
//
//	// 在这里添加实际的任务处理逻辑
//
//	logger.Info(fmt.Sprintf("[%s] 任务执行完成", s.String()), interceptor.ServerCtxRequestIDField(ctx))
//	return nil
//}
