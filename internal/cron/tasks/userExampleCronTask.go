// Package tasks 包含定时任务实现
package tasks

//
//import (
//	"context"
//	"fmt"
//	"runtime/debug"
//	"strings"
//	"time"
//
//	"github.com/18721889353/sunshine/internal/cache"
//	"github.com/18721889353/sunshine/internal/cron"
//	"github.com/18721889353/sunshine/internal/dao"
//	"github.com/18721889353/sunshine/internal/database"
//	"github.com/18721889353/sunshine/pkg/gocron"
//	"github.com/18721889353/sunshine/pkg/logger"
//	"github.com/go-redsync/redsync/v4"
//	"google.golang.org/grpc/metadata"
//)
//
//// 常量定义
//const (
//	taskName      = "userExampleCronTask"
//	lockExpiry    = 10 * time.Minute // 分布式锁过期时间
//	taskTimeout   = 5 * time.Minute  // 任务执行超时时间
//	lockKeyPrefix = "cron:lock:"     // 分布式锁Key前缀
//)
//
//func init() {
//	cron.RegisterTask(&gocron.Task{
//		Name:      taskName,
//		TimeSpec:  gocron.EveryHour(4),
//		Fn:        newCronTasksUserExampleService().userExampleCronTask,
//		IsRunOnce: false,
//	})
//}
//
//type userExampleCronTaskService struct {
//	iTkUserExampleDao dao.UserExampleDao
//	cache             cache.UserExampleCache
//}
//
//func newCronTasksUserExampleService() *userExampleCronTaskService {
//	return &userExampleCronTaskService{
//		iTkUserExampleDao: dao.NewUserExampleDao(
//			database.GetDB(),
//			cache.NewUserExampleCache(database.GetCacheType()),
//		),
//		cache: cache.NewUserExampleCache(database.GetCacheType()),
//	}
//}
//
//func (s *userExampleCronTaskService) String() string {
//	return taskName
//}
//
//func (s *userExampleCronTaskService) userExampleCronTask() {
//	// 创建带RequestID的上下文
//	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
//		string(logger.ContextKeyRequestID): database.GetSnowID().String(),
//	}))
//
//	// 使用trace装饰器包裹整个任务执行流程
//	_ = s.trace(ctx, taskName, func() error {
//		// ========== Panic 恢复机制 ==========
//		// 标准:必须在最外层注册defer recover,防止panic导致消费者进程崩溃
//		defer func() {
//			if r := recover(); r != nil {
//				// 使用debug.Stack()获取完整的堆栈信息,便于问题排查
//				logger.ErrorWithCtx(ctx, fmt.Sprintf("[%s] panic recovered: %v\nstack: %s", taskName, r, string(debug.Stack())))
//			}
//		}()
//
//		// ========== 创建超时上下文 ==========
//		// 超时时间应该大于锁过期时间,确保有足够时间完成任务
//		timeoutCtx, cancel := context.WithTimeout(ctx, taskTimeout+lockExpiry)
//		defer cancel()
//
//		// ========== 记录任务开始时间 ==========
//		startTime := time.Now()
//
//		// ========== 获取分布式锁并执行业务逻辑 ==========
//		// 使用WatchDogLock: 自动续期机制,防止长任务执行期间锁过期
//		lockKey := lockKeyPrefix + taskName
//		err := s.cache.WatchDogLock(timeoutCtx, lockKey, lockExpiry,
//			func(watchdogCtx context.Context) error {
//				// 在锁保护下执行业务逻辑
//				logger.InfoWithCtx(watchdogCtx, "["+taskName+"] 获取分布式锁成功,开始执行任务")
//				return s.processUserExamples(watchdogCtx)
//			},
//			redsync.WithExpiry(lockExpiry),               // 锁的初始过期时间
//			redsync.WithRetryDelay(time.Millisecond*100), // 重试间隔
//			redsync.WithTries(50),                        // 最大重试次数(总计等待约 5 秒)
//		)
//
//		if err != nil {
//			if strings.Contains(err.Error(), "lock already taken") {
//				logger.InfoWithCtx(ctx, "["+taskName+"] 锁已被占用,跳过本次执行")
//				return nil // 锁被占用不算错误,直接返回
//			}
//			// 其他错误需要返回,触发重试或记录失败
//			logger.WarnWithCtx(ctx, "["+taskName+"] 获取分布式锁或执行任务失败",
//				logger.Err(err),
//				logger.String("duration", fmt.Sprintf("%.2fs", time.Since(startTime).Seconds())))
//			return err
//		}
//
//		// ========== 记录任务成功完成 ==========
//		logger.InfoWithCtx(ctx, "["+taskName+"] 任务执行成功",
//			logger.String("duration", fmt.Sprintf("%.2fs", time.Since(startTime).Seconds())))
//
//		return nil
//	})
//}
//
//// trace 耗时监控装饰器
//// 标准实践:所有关键业务操作都应该有耗时监控
//// 参数:
////   - ctx: 上下文对象,用于日志记录
////   - name: 操作名称,用于日志标识(建议使用 "模块名:操作名" 格式)
////   - fn: 要执行的业务逻辑函数
////
//// 返回:
////   - error: 业务逻辑函数的返回值,原样返回
//func (s *userExampleCronTaskService) trace(ctx context.Context, name string, fn func() error) error {
//	startTime := time.Now()
//	err := fn()
//	duration := time.Since(startTime)
//
//	// 构建日志字段
//	fields := []logger.Field{
//		logger.String("ms", fmt.Sprintf("%.4f", float64(duration.Nanoseconds())/1e6)), // 毫秒浮点数,便于SLS数值查询
//	}
//
//	// 根据执行结果记录不同级别的日志
//	if err != nil {
//		fields = append(fields, logger.Err(err))
//		logger.WarnWithCtx(ctx, name+"(失败)", fields...)
//	} else {
//		logger.InfoWithCtx(ctx, name+"(成功)", fields...)
//	}
//	return err
//}
//
//func (s *userExampleCronTaskService) processUserExamples(ctx context.Context) error {
//	// 使用trace装饰器包裹业务逻辑,自动记录耗时
//	return s.trace(ctx, taskName+":processUserExamples", func() error {
//		// TODO: 在这里添加实际的任务处理逻辑
//		// 示例:
//		// - 查询需要处理的数据
//		// - 批量处理业务逻辑
//		// - 更新缓存
//		// - 发送通知等
//
//		_ = s.iTkUserExampleDao // 使用已初始化的DAO
//		_ = s.cache             // 使用已初始化的Cache
//
//		return nil
//	})
//}
