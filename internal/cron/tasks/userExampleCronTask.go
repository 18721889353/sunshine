// Package tasks 定时任务集合
package tasks

//
//import (
//	"context"
//	"encoding/json"
//	"fmt"
//	"runtime"
//	"strconv"
//	"strings"
//	"time"
//
//	"google.golang.org/grpc/metadata"
//
//	"github.com/18721889353/sunshine/internal/cache"
//	"github.com/18721889353/sunshine/internal/cron"
//	"github.com/18721889353/sunshine/internal/dao"
//	"github.com/18721889353/sunshine/internal/database"
//	"github.com/18721889353/sunshine/pkg/gocron"
//	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
//	"github.com/18721889353/sunshine/pkg/logger"
//)
//
//func init() {
//	cron.RegisterTask(&gocron.Task{
//		Name:      newUserExampleTaskService().String(),
//		TimeSpec:  gocron.EveryMinute(1),
//		Fn:        newUserExampleTaskService().userExampleTask,
//		IsRunOnce: false,
//	})
//}
//
//// 并发控制：本地 isRunning 互斥 + 分布式锁(多实例) + 50s 执行超时(小于1分钟任务周期)
//type userExampleTaskService struct {
//	isRunning       bool
//	cache           cache.UserExampleCache
//	iUserExampleDao *dao.UserExampleDao // 具体类型：需调用 ExecByCustomFunc 执行事务更新（接口未暴露该方法）
//}
//
//func newUserExampleTaskService() *userExampleTaskService {
//	return &userExampleTaskService{
//		isRunning: false, // 初始化时任务未运行中
//	}
//}
//
//func (s *userExampleTaskService) String() string {
//	return "userExampleTask"
//}
//
//// trace 统一执行日志包装：记录耗时(ms)，失败/慢任务走 Warn 级别上报 SLS，成功走 Info。
//// 替代手写的“开始/完成/失败”三段式日志，让每次执行的耗时可查询、慢任务可告警。
//// 慢阈值 3s：任务型执行正常在 1~2 秒（非 MQ 的 200ms），超过即偏慢（慢查询/锁等待）值得排查。
//func (s *userExampleTaskService) trace(ctx context.Context, name string, fn func() error, params ...logger.Field) error {
//	startTime := time.Now()
//	err := fn()
//	duration := time.Since(startTime)
//
//	// 1. 使用栈上缓冲区避免堆分配，减少 allocs
//	var msBuf [16]byte
//	ms := strconv.AppendFloat(msBuf[:0], float64(duration.Nanoseconds())/1e6, 'f', 4, 64)
//	fields := []logger.Field{
//		logger.String("ms", string(ms)), // 毫秒浮点数，便于SLS数值查询
//	}
//	fields = append(fields, params...)
//
//	// 2. 按执行结果分级输出：失败/慢任务 Warn 上报 SLS，正常 Info 落本地文件
//	switch {
//	case err != nil:
//		fields = append(fields, logger.Err(err))
//		logger.WarnWithCtx(ctx, name+"(失败)", fields...)
//	case duration.Milliseconds() > 3000: // 3秒
//		logger.WarnWithCtx(ctx, name+"(慢任务)", fields...)
//	default:
//		logger.InfoWithCtx(ctx, name+"(成功)", fields...)
//	}
//	return err
//}
//
//// getErrorWithLine 获取带行号的错误信息：错误消息前缀 file:line，快速定位出错代码行。
////   - err 为 nil 时返回 nil
////   - runtime.Caller 失败时返回原始错误（不吞错）
////   - 用 %w 保留原始错误链，上层 errors.Is/As 判断不受影响
//func (s *userExampleTaskService) getErrorWithLine(err error, params ...map[string]any) error { //nolint:unused // 示例辅助函数，待 logic 实现具体错误返回后使用
//	// 1. 无错误直接返回
//	if err == nil {
//		return nil
//	}
//
//	// 2. 获取调用方行号，失败时直接返回原始错误
//	_, file, line, ok := runtime.Caller(1)
//	if !ok {
//		return err
//	}
//
//	// 3. 有业务上下文时一并序列化进错误消息
//	if len(params) > 0 {
//		paramBytes, marshalErr := json.Marshal(params)
//		if marshalErr != nil {
//			return fmt.Errorf("%s:%d: marshal params error: %v, original error: %w", file, line, marshalErr, err)
//		}
//		return fmt.Errorf("%s:%d: params:%s, error:%w", file, line, string(paramBytes), err)
//	}
//	return fmt.Errorf("%s:%d: error:%w", file, line, err)
//}
//
//// cancelOrderTimeoutTask 订单超时取消任务入口
//func (s *userExampleTaskService) userExampleTask() {
//	// 先使用本地互斥锁进行快速检查
//	if s.isRunning {
//		logger.InfoWithCtx(context.Background(), s.String()+"任务已在运行中，跳过本次执行")
//		return
//	}
//	s.isRunning = true
//	// 确保在函数结束时重置运行状态
//	defer func() {
//		s.isRunning = false
//	}()
//
//	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
//		string(logger.ContextKeyRequestID): database.GetSnowID().String(),
//	}))
//	// 超时时间需小于任务周期(1分钟)，避免跨周期占用
//	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
//	defer cancel()
//
//	ctx = timeoutCtx
//	// 初始化 DAO 和缓存实例
//	s.cache = cache.NewUserExampleCache(database.GetCacheType())
//	s.iUserExampleDao = dao.NewUserExampleDao(
//		database.GetDB(),
//		cache.NewUserExampleCache(database.GetCacheType()),
//	)
//
//	logger.InfoWithCtx(ctx, s.String()+"开始获取分布式锁", interceptor.ServerCtxRequestIDField(ctx))
//	{
//		// 获取分布式锁
//		mutex, err := s.cache.GetLock(ctx, s.String()+"LockKey")
//		if err != nil {
//			if strings.Contains(err.Error(), "lock already taken") {
//				logger.InfoWithCtx(ctx, s.String()+"锁已被占用，跳过本次执行", interceptor.ServerCtxRequestIDField(ctx))
//				return
//			}
//			logger.WarnWithCtx(ctx, s.String()+"获取分布式锁失败", logger.Err(err), interceptor.ServerCtxRequestIDField(ctx))
//			// 如果获取锁失败，直接返回，不执行任务
//			return
//		}
//		// 确保在函数结束时释放分布式锁
//		defer func() {
//			if _, releaseErr := mutex.UnlockContext(ctx); releaseErr != nil {
//				logger.WarnWithCtx(ctx, s.String()+"释放分布式锁失败", logger.Err(releaseErr), interceptor.ServerCtxRequestIDField(ctx))
//			} else {
//				logger.InfoWithCtx(ctx, s.String()+"分布式锁释放成功", interceptor.ServerCtxRequestIDField(ctx))
//			}
//		}()
//	}
//
//	logger.InfoWithCtx(ctx, s.String()+"获取分布式锁成功，开始执行任务", interceptor.ServerCtxRequestIDField(ctx))
//	// 执行实际的处理任务
//
//	// 失败日志由 logic 内的 trace 输出（带耗时，Warn 上报 SLS），此处不再重复打印
//	_ = s.logic(ctx) //nolint:errcheck // 错误已由 trace 记录，有意丢弃
//
//	logger.InfoWithCtx(ctx, s.String()+"任务执行完成", interceptor.ServerCtxRequestIDField(ctx))
//}
//
//// logic 任务逻辑
//// 实现时用 s.trace 包裹主体（统一耗时/结果日志），错误返回点用 s.getErrorWithLine 包装（带行号定位）。
//func (s *userExampleTaskService) logic(ctx context.Context) error {
//	return s.trace(ctx, s.String()+":logic", func() error {
//		//todo: 实现具体逻辑；错误返回示例：
//		return nil
//	},
//		logger.String("remark", "任务逻辑描述(供SLS日志检索)"),
//	)
//}
