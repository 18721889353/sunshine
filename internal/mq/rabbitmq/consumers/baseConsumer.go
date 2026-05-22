// Package consumers  基类
package consumers

//import (
//	"context"
//	"encoding/json"
//	"fmt"
//	"github.com/18721889353/sunshine/internal/cache"
//	"github.com/18721889353/sunshine/internal/consts"
//	"github.com/18721889353/sunshine/internal/dao"
//	"github.com/18721889353/sunshine/internal/database"
//	"net/http"
//	"reflect"
//	"runtime"
//	"runtime/debug"
//	"sync"
//	"time"
//
//	"github.com/go-redsync/redsync/v4"
//
//	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq/gorabbitmqconsumer"
//	"github.com/18721889353/sunshine/pkg/gohttp"
//	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
//	"github.com/18721889353/sunshine/pkg/logger"
//	"google.golang.org/grpc/metadata"
//)
//
//// BaseMQConsumer MQ消费者基础类，提供公共方法
//type BaseMQConsumer struct {
//	*gorabbitmqconsumer.BaseConsumer
//	iUserExampleDao dao.UserExampleDao
//	iCache          cache.UserExampleCache
//	httpClient      *gohttp.Client
//	initOnce        sync.Once // 确保只初始化一次
//	// 重试策略函数指针，子类可以覆盖此函数实现自定义重试逻辑
//	retryStrategyFunc func(ctx context.Context, orderSn, busType string, currentErr error, maxErrorNum int64) error
//}
//
//// initBaseMQConsumer 初始化基础MQ消费者（线程安全，只执行一次）
//func (b *BaseMQConsumer) initBaseMQConsumer() {
//	b.initOnce.Do(func() {
//		// 确保数据库已初始化
//		db := database.GetDB()
//		if db == nil {
//			panic("database not initialized, please check database configuration and initialization")
//		}
//
//		b.iUserExampleDao = dao.NewUserExampleDao(db, nil)
//		b.iCache = cache.NewUserExampleCache(database.GetCacheType())
//		b.httpClient = gohttp.New(
//			gohttp.WithBaseURL(""),
//			gohttp.WithTimeout(10*time.Second),
//			gohttp.WithTransport(&http.Transport{
//				// 1. 总空闲连接数：设为 500
//				// 确保即使访问多个域名，整体连接池也有足够余量
//				MaxIdleConns: 500,
//				// 2. 每个主机的最大空闲连接数：关键调优项，设为 200 或更高
//				// 匹配你的并发数。这样 200 个并发请求完成后，连接会全部进入池中缓存
//				// 不再频繁触发连接的销毁与重建
//				MaxIdleConnsPerHost: 200,
//				// 3. 空闲连接超时时间：设为 90 秒
//				// 适当延长可以跨越业务的低谷期，减少因为连接失效导致的重连
//				IdleConnTimeout: 90 * time.Second,
//				// 4. TLS 握手超时时间：保持 10 秒
//				// 这是一个合理的防御性阈值，防止在网络握手阶段无限挂起协程
//				TLSHandshakeTimeout: 10 * time.Second,
//				// 5. 期待继续超时：保持 1 秒
//				// 默认推荐值，优化大数据量 POST 时的交互性能
//				ExpectContinueTimeout: 1 * time.Second,
//				// 6. 响应头读取超时：设为 8 秒
//				// 防止服务器响应缓慢时，客户端在读取响应头阶段无限等待
//				ResponseHeaderTimeout: 8 * time.Second,
//				// 7. 强制尝试 HTTP/2：启用
//				// 如果服务器支持 HTTP/2，自动升级协议以获得更好的性能和多路复用能力
//				ForceAttemptHTTP2: true,
//			}),
//		)
//		// 默认使用基类的重试策略
//		b.retryStrategyFunc = b.defaultRetryStrategy
//	})
//}
//
//// getErrorWithLine 获取带行号的错误信息
//func (b *BaseMQConsumer) getErrorWithLine(err error, params ...map[string]any) error {
//	if err != nil {
//		// 防御性检查：处理接口值为 nil 的情况
//		if reflect.ValueOf(err).IsNil() {
//			return nil
//		}
//		_, file, line, ok := runtime.Caller(1)
//		if ok {
//			paramBytes, marshalErr := json.Marshal(params)
//			if marshalErr != nil {
//				return fmt.Errorf("%s:%d: marshal params error: %v, original error: %v", file, line, marshalErr, err)
//			}
//			return fmt.Errorf("%s:%d: params:%s, error:%w", file, line, string(paramBytes), err)
//		}
//	}
//	return nil
//}
//
//// RecordMqLog 记录MQ执行日志
//func (b *BaseMQConsumer) recordMqLog(ctx context.Context, paramsByte []byte, orderSn string, startTime time.Time, err error) {
//	//status := consts.MqLogStatusSuccess
//	//errMsg := consts.BusMQLogStatusSuccess
//	//if err != nil {
//	//	status = consts.MqLogStatusFailed
//	//	errMsg = err.Error()
//	//}
//	//if createErr := b.iUserExampleDao.Create(ctx, &model.UserExample{
//	//	Params:    string(paramsByte),
//	//	DataFrom:  b.Name(),
//	//	RequestID: interceptor.ServerCtxRequestIDField(ctx).String,
//	//	OrderSn:   orderSn,
//	//	Status:    status,
//	//	ErrMsg:    errMsg,
//	//	StartTime: startTime,
//	//	SpendTime: time.Since(startTime).Milliseconds(),
//	//}); createErr != nil {
//	//	logger.WarnWithCtx(ctx, b.Name()+":记录MQ日志失败", logger.Err(createErr))
//	//}
//}
//
//// defaultRetryStrategy 默认重试策略（基类实现）
//func (b *BaseMQConsumer) defaultRetryStrategy(ctx context.Context, orderSn, busType string, currentErr error, maxErrorNum int64) error {
//	if orderSn == "" || currentErr == nil {
//		return currentErr
//	}
//
//	errorNum, successNum, err := b.getMqLogNum(ctx, orderSn)
//	if err != nil {
//		logger.WarnWithCtx(ctx, b.Name()+":获取MQ日志数量失败", logger.Err(err))
//		return currentErr
//	}
//	// 如果历史上已经成功过，本次错误不再重试
//	if successNum >= 1 {
//		return nil
//	}
//	// 如果错误次数超过阈值，记录警告并停止重试（Ack）
//	if errorNum > maxErrorNum {
//		logger.WarnWithCtx(ctx, b.Name()+":错误次数过多，停止重试", logger.Err(currentErr))
//		return nil
//	}
//	return currentErr
//}
//
//// handleRetryStrategy 处理重试与错误屏蔽策略（支持子类覆盖）
//func (b *BaseMQConsumer) handleRetryStrategy(ctx context.Context, orderSn, busType string, currentErr error, maxErrorNum int64) error {
//	// 如果子类设置了自定义重试策略，则使用子类的；否则使用默认的
//	if b.retryStrategyFunc != nil {
//		return b.retryStrategyFunc(ctx, orderSn, busType, currentErr, maxErrorNum)
//	}
//	return b.defaultRetryStrategy(ctx, orderSn, busType, currentErr, maxErrorNum)
//}
//
//// Trace 耗时监控装饰器
//func (b *BaseMQConsumer) trace(ctx context.Context, name string, fn func() error) error {
//	startTime := time.Now()
//	err := fn()
//	duration := time.Since(startTime)
//
//	fields := []logger.Field{
//		logger.String("ms", fmt.Sprintf("%.4f", float64(duration.Nanoseconds())/1e6)), // 毫秒浮点数，便于SLS数值查询
//	}
//
//	if err != nil {
//		fields = append(fields, logger.Err(err))
//		logger.WarnWithCtx(ctx, name+"(失败)", fields...)
//	} else {
//		logger.InfoWithCtx(ctx, name+"(成功)", fields...)
//	}
//	return err
//}
//
//// GetMqLogNum 获取MQ日志数量统计
//func (b *BaseMQConsumer) getMqLogNum(ctx context.Context, orderSn string) (errorNum, successNum int64, err error) {
//	err = b.trace(ctx, b.Name()+":getMqLogNum", func() error {
//		//var result []map[string]interface{}
//		//_, queryErr := b.iCpMqLogDao.GetByCustomQuery(ctx, func(db *gorm.DB) *gorm.DB {
//		//	return db.Table("cp_mq_log").
//		//		Select("status, COUNT(*) as count").
//		//		Where("order_sn = ? AND data_from = ?", orderSn, b.Name()).
//		//		Group("status")
//		//}, &result, -1, 0)
//		//
//		//if queryErr != nil {
//		//	return b.getErrorWithLine(queryErr)
//		//}
//		//
//		//// 解析结果
//		//for _, row := range result {
//		//	status := cast.ToInt64(row["status"])
//		//	count := cast.ToInt64(row["count"])
//		//	if status == 1 {
//		//		successNum = count
//		//	} else if status == 2 {
//		//		errorNum = count
//		//	}
//		//}
//		return nil
//	})
//	return errorNum, successNum, b.getErrorWithLine(err)
//}
//
//// HandleMessageTemplate MQ消息处理模板方法 (模板方法模式)
//// 子类只需实现 extractOrderSn 和 parseAndExecute 函数即可
////
//// 参数说明:
////   - ctx: 上下文
////   - data: 原始消息数据
////   - messageId: 消息ID
////   - extractOrderSn: 轻量级提取 orderSn 和 busType 的函数（只解析JSON字段，不执行业务）
////   - parseAndExecute: 解析并执行业务逻辑的函数，负责 Unmarshal、Validate 和业务处理
////   - useLock: 是否使用分布式锁（锁key格式: LockKey:{ConsumerName}:{OrderSn}）
////   - maxErrorNum: 最大错误重试次数
//func (b *BaseMQConsumer) HandleMessageTemplate(
//	ctx context.Context,
//	data []byte,
//	messageId string,
//	extractOrderSn func(data []byte) (orderSn string, busType string, err error),
//	parseAndExecute func(ctx context.Context, data []byte) (orderSn string, busType string, err error),
//	useLock bool,
//	maxErrorNum int64,
//) (err error) {
//	startTime := time.Now()
//	if !database.IsSnowflakeID(messageId) {
//		messageId = database.GetSnowID().String()
//	}
//
//	// 1. 初始化 Context 和 RequestID
//	ctx = metadata.NewIncomingContext(ctx, metadata.New(map[string]string{
//		string(logger.ContextKeyRequestID): messageId,
//	}))
//	ctx = interceptor.WrapServerCtx(ctx)
//	ctx = logger.WithCallerFunc(ctx, b.Name())
//
//	logger.InfoWithCtx(ctx, b.Name()+":开始处理消息")
//
//	var orderSn string
//	var busType string
//
//	// 2. 统一处理收尾工作 (Panic, 日志落库, 重试控制)
//	defer func() {
//		if r := recover(); r != nil {
//			// 使用 debug.Stack() 获取堆栈信息并保持原始格式
//			logger.WarnWithCtx(ctx, fmt.Sprintf(b.Name()+":panic recovered: %v\nstack: %s", r, string(debug.Stack())))
//			err = fmt.Errorf("panic recovered: %v", r)
//		}
//		// 【关键修复】使用 WithoutCancel 确保日志一定写入，即使 ctx 被取消
//		logCtx := context.WithoutCancel(ctx)
//		// 记录数据库日志 (建议在 trace 之外，确保无论如何都记录)
//		b.recordMqLog(logCtx, data, orderSn, startTime, err)
//		// 核心策略：处理重试控制 (判断 successNum 和 errorNum)
//		// 注意：这里会将 err 修改为 nil 以实现特定条件下的 Ack
//		err = b.handleRetryStrategy(logCtx, orderSn, busType, err, maxErrorNum)
//	}()
//
//	// 3. 使用 trace 装饰器包裹主体逻辑
//	return b.trace(ctx, b.Name()+":HandleMessage", func() error {
//		// A. 基础校验
//		if b == nil || messageId == "" || data == nil {
//			return b.getErrorWithLine(fmt.Errorf("初始化异常或参数缺失"))
//		}
//
//		// B. 执行业务逻辑（可选分布式锁）
//		if useLock {
//			// 第一步：轻量级提取 orderSn 用于生成锁 key（只解析JSON字段，不执行业务）
//			tempOrderSn, tempBusType, extractErr := extractOrderSn(data)
//			if extractErr != nil {
//				orderSn = ""
//				busType = ""
//				return b.getErrorWithLine(extractErr)
//			}
//			orderSn = tempOrderSn
//			busType = tempBusType
//
//			// 第二步：生成统一的锁 key: LockKey:{ConsumerName}:{OrderSn}
//			lockKey := fmt.Sprintf("LockKey:%s:%s", b.Name(), orderSn)
//			expiry := time.Second * consts.LockExpirySeconds
//			return b.iCache.WatchDogLoopLock(ctx, lockKey, expiry,
//				func(watchdogCtx context.Context) error {
//					// 第三步：锁内完整解析并执行业务逻辑（JSON只解析一次，业务只执行一次）
//					finalOrderSn, finalBusType, execErr := parseAndExecute(watchdogCtx, data)
//					// 更新外层的 orderSn 和 busType（用于 defer 中的日志记录）
//					if finalOrderSn != "" {
//						orderSn = finalOrderSn
//					}
//					if finalBusType != "" {
//						busType = finalBusType
//					}
//					return execErr
//				},
//				redsync.WithExpiry(expiry),
//				redsync.WithRetryDelay(time.Millisecond*consts.LockRetryDelayMs),
//				redsync.WithTries(consts.LockMaxTries),
//			)
//		}
//
//		// C. 无锁模式，直接执行
//		orderSn, busType, err = parseAndExecute(ctx, data)
//		return b.getErrorWithLine(err)
//	})
//}
