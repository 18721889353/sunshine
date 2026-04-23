// Package consumers 提供应用程序的 RabbitMQ 消息消费者。
package consumers

//
//import (
//	"context"
//	"encoding/json"
//	"errors"
//	"fmt"
//	"runtime"
//	"runtime/debug"
//	"time"
//
//	"gorm.io/gorm"
//
//	"github.com/18721889353/sunshine/internal/cache"
//	"github.com/18721889353/sunshine/internal/dao"
//   "google.golang.org/grpc/metadata"

//	"github.com/18721889353/sunshine/internal/config"
//	"github.com/18721889353/sunshine/internal/database"
//	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"
//	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq/gorabbitmqConsumer"
//	"github.com/go-redsync/redsync/v4"
//
//	"github.com/18721889353/sunshine/pkg/httpcli"
//	"github.com/18721889353/sunshine/pkg/logger"
//	"github.com/spf13/cast"
//)
//
//// init 包初始化函数
//// 在包被导入时自动执行，用于注册消费者到全局注册表
////
//// 标准实践：
//// 1. 使用 init 函数自动注册消费者，避免手动注册的遗漏
//// 2. 消费者名称应该具有唯一性和可读性
//// 3. HandleMessage 方法是消费者的核心处理逻辑
//func init() {
//	// 实例化订单消费者对象
//	oc := &orderConsumer{}
//
//	// 创建基础消费者，传入消费者名称和消息处理函数
//	oc.BaseConsumer = gorabbitmqConsumer.NewBaseConsumer(
//		"doingOrderMqConsumer", // 消费者名称，用于日志和监控标识
//		oc.HandleMessage,       // 消息处理函数
//	)
//
//	// 注册到全局注册表（mq.go 中的 registry）
//	// 这样在应用启动时，所有注册的消费者会自动启动
//	mq.RegisterConsumer(oc)
//}
//
//// OrderMQParam 订单消息入参结构体
//// 用于接收 RabbitMQ 消息中的订单数据
//type OrderMQParam struct {
//	Source  string `json:"source"`   // 数据来源标识
//	BusType string `json:"bus_type"` // 业务类型
//	OrderSn string `json:"order_sn"` // 订单号
//}
//
//// Validate 验证订单消息参数的合法性
//// 检查所有必填字段是否为空
//// 返回:
////   - error: 如果验证失败，返回具体的错误信息；否则返回 nil
//func (b *OrderMQParam) Validate() error {
//	if b.Source == "" {
//		return errors.New("source 不能为空")
//	}
//	if b.BusType == "" {
//		return errors.New("bus_type 不能为空")
//	}
//	if b.OrderSn == "" {
//		return errors.New("order_sn 不能为空")
//	}
//	return nil
//}
//
//// orderConsumer 订单消费者结构体
//// 继承 BaseConsumer，实现订单消息的具体处理逻辑
//type orderConsumer struct {
//	*gorabbitmqConsumer.BaseConsumer                        // 嵌入基础消费者，复用通用功能
//	iUserExampleDao                  dao.UserExampleDao     // 数据访问对象（示例）
//	iCache                           cache.UserExampleCache // 缓存对象（用于分布式锁）
//	httpClient                       *httpcli.Client        // HTTP 客户端（预留，可用于调用外部服务）
//}
//
//// Start 启动订单消费者
//// 初始化消费者所需的依赖组件，并启动消费流程
//// 参数:
////   - ctx: 上下文对象，用于控制启动过程的生命周期
////
//// 返回:
////   - error: 启动过程中的错误，包括连接错误、配置错误等
//func (s *orderConsumer) Start(ctx context.Context) error {
//	// 1. 初始化数据访问层
//	s.iUserExampleDao = dao.NewUserExampleDao(
//		database.GetDB(),
//		nil,
//	)
//
//	// 2. 初始化缓存层（用于分布式锁）
//	s.iCache = cache.NewUserExampleCache(database.GetCacheType())
//
//	// 3. 从数据库配置中获取 RabbitMQ 连接
//	conn, err := database.GetMainRabbitMQ().GetConnection(ctx)
//	if err != nil {
//		return fmt.Errorf(s.Name()+":get connection error: %w", err)
//	}
//
//	// 4. 调用基础消费者的 Start 方法，开始消费消息
//	return s.BaseConsumer.Start(ctx, conn, config.Get().Rabbitmq.DoingOrder)
//}
//
//// getErrorWithLine 获取带文件位置信息的错误
//// 用于在错误信息中包含出错的文件名和行号，便于快速定位问题
//// 参数:
////   - err: 原始错误对象
////   - params: 可选的参数 map，会被序列化为 JSON 附加到错误信息中
////
//// 返回:
////   - error: 包装后的错误，包含文件位置、参数信息和原始错误
//func (s *orderConsumer) getErrorWithLine(err error, params ...map[string]any) error {
//	if err != nil {
//		// 获取调用者的文件位置（跳过当前函数，获取上一层调用位置）
//		_, file, line, ok := runtime.Caller(1)
//		if ok {
//			paramBytes, _ := json.Marshal(params)
//			return fmt.Errorf("%s:%d: params:%s, error:%w", file, line, string(paramBytes), err)
//		}
//	}
//	return nil
//}
//
//// trace 耗时监控装饰器
//// 包裹业务逻辑函数，自动记录执行时间和成功/失败状态
//// 标准实践：所有关键业务操作都应该有耗时监控
//// 参数:
////   - ctx: 上下文对象，用于日志记录
////   - name: 操作名称，用于日志标识（建议使用 "模块名:操作名" 格式）
////   - fn: 要执行的业务逻辑函数
////
//// 返回:
////   - error: 业务逻辑函数的返回值，原样返回
//func (s *orderConsumer) trace(ctx context.Context, name string, fn func() error) error {
//	startTime := time.Now()
//	err := fn()
//	duration := time.Since(startTime)
//
//	// 构建日志字段
//	fields := []logger.Field{
//      logger.String("ms", fmt.Sprintf("%.4f", float64(duration.Nanoseconds())/1e6)), // 毫秒浮点数，便于SLS数值查询
//	}
//
//	// 根据执行结果记录不同级别的日志
//	if err != nil {
//		fields = append(fields, logger.Error(err))
//		logger.WarnWithCtx(ctx, name+"(失败)", fields...)
//	} else {
//		logger.InfoWithCtx(ctx, name+"(成功)", fields...)
//	}
//	return err
//}
//
//// HandleMessage 处理订单消息的核心方法
//// 这是 MQ 消费者的入口函数，由底层框架调用
////
//// 标准处理流程：
//// 1. 初始化 Context（注入 RequestID，用于全链路追踪）
//// 2. 注册 Panic 恢复机制（防止单个消息异常导致消费者崩溃）
//// 3. 使用 trace 装饰器包裹业务逻辑（自动记录耗时）
//// 4. 参数校验和数据解析
//// 5. 使用分布式锁防止重复消费（幂等性保证）
//// 6. 执行业务逻辑
////
//// 参数:
////   - ctx: 上下文对象
////   - data: 消息体字节数组（JSON 格式）
////   - messageId: 消息 ID，用作 RequestID 和幂等性标识
////   - tagID: 消息标签 ID，用于调试和追踪
////
//// 返回:
////   - err: 处理错误，如果返回非 nil 错误，消息会被重新投递或进入死信队列
//func (s *orderConsumer) HandleMessage(ctx context.Context, data []byte, messageId, tagID string) (err error) {
//// 1. 初始化 Context 和 RequestID
//ctx = metadata.NewIncomingContext(ctx, metadata.New(map[string]string{
//string(logger.ContextKeyRequestID): messageId,
//}))
//ctx = interceptor.WrapServerCtx(ctx)
//ctx = glog.WithCallerFunc(ctx, s.Name())
//	var orderSn string // 订单号，用于后续日志记录和分布式锁
//
//	// ========== 步骤 2: 统一处理收尾工作（Panic 恢复） ==========
//	// 标准：必须在最外层注册 defer recover，防止 panic 导致消费者进程崩溃
//	defer func() {
//		if r := recover(); r != nil {
//			// 使用 debug.Stack() 获取完整的堆栈信息，便于问题排查
//			logger.ErrorWithCtx(ctx, fmt.Sprintf(s.Name()+":panic recovered: %v\nstack: %s", r, string(debug.Stack())))
//			err = fmt.Errorf("panic recovered: %v", r)
//		}
//	}()
//
//	// ========== 步骤 3: 使用 trace 装饰器包裹主体逻辑 ==========
//	// 自动记录执行时间、成功/失败状态
//	return s.trace(ctx, s.Name()+":HandleMessage", func() error {
//		// ===== A. 基础校验 =====
//		// 防御性编程：检查必要的参数和对象状态
//		if s == nil || messageId == "" || data == nil {
//			return s.getErrorWithLine(errors.New("初始化异常或参数缺失"))
//		}
//
//		// ===== B. 解析与验证 =====
//		// 将 JSON 消息体反序列化为结构体
//		orderMQParam := &OrderMQParam{}
//		if err = json.Unmarshal(data, orderMQParam); err != nil {
//			// JSON 解析失败，通常是消息格式错误，不应该重试
//			return s.getErrorWithLine(fmt.Errorf("json.Unmarshal failed: %w", err))
//		}
//
//		// 先赋值 orderSn，确保即便 Validate 失败，defer 也能记下单号
//		orderSn = orderMQParam.OrderSn
//
//		// 验证业务参数的合法性
//		if err = orderMQParam.Validate(); err != nil {
//			// 参数验证失败，不应该重试
//			return s.getErrorWithLine(fmt.Errorf("validation failed: %w", err))
//		}
//
//		// ===== C. 分布式锁防止重复消费 =====
//		// 标准：订单处理必须保证幂等性，使用分布式锁防止同一订单被重复处理
//		lockKey := "LockKey:handleOrderMessage:OrderSn:" + cast.ToString(orderSn)
//		expiry := time.Second * 6 // 锁的过期时间，应该大于业务处理的最大预期时间
//
//		// WatchDogLoopLock: 看门狗模式的分布式锁
//		// - 自动续期：在锁持有期间自动续期，防止业务未执行完锁就过期
//		// - 重试机制：获取锁失败时会自动重试
//		return s.iCache.WatchDogLoopLock(ctx, lockKey, expiry,
//			func(ctx context.Context) error {
//				// 在锁保护下执行真正的业务逻辑
//				return s.logic(ctx, orderMQParam)
//			},
//			redsync.WithExpiry(expiry),                  // 锁的初始过期时间
//			redsync.WithRetryDelay(time.Millisecond*25), // 重试间隔
//			redsync.WithTries(400),                      // 最大重试次数（总计等待约 10 秒）
//		)
//	})
//}
//
//// logic 订单业务逻辑处理方法
//// 在这里实现具体的订单处理业务，例如：
//// - 更新订单状态
//// - 发送通知
//// - 调用外部服务
//// - 记录业务日志
////
//// 参数:
////   - ctx: 上下文对象，包含 RequestID 等信息
////   - orderMQParam: 解析后的订单消息参数
////
//// 返回:
////   - error: 业务处理错误，如果返回错误，分布式锁会释放，消息可能会重试
//func (s *orderConsumer) logic(ctx context.Context, orderMQParam *OrderMQParam) (err error) {
//	// 使用 trace 装饰器记录业务逻辑的耗时
//	return s.trace(ctx, s.Name()+":logic", func() error {
//		// TODO: 在这里实现具体的业务逻辑
//		// 示例：
//		// 1. 查询订单信息
//		// 2. 更新订单状态
//		// 3. 发送通知
//		// 4. 记录业务日志
//
//		// ========== 数据库事务处理 ==========
//		// 标准：所有写操作必须在事务中执行，保证数据一致性
//		err = s.iUserExampleDao.ExecByCustomFunc(ctx, func(db *gorm.DB) *gorm.DB {
//			// 在事务中执行数据库操作
//			txErr := db.Transaction(func(tx *gorm.DB) error {
//				// 设置 InnoDB 锁等待超时时间（5秒）
//				// 防止长时间锁等待导致雪崩效应
//				if err := tx.Exec("SET SESSION innodb_lock_wait_timeout = 5").Error; err != nil {
//					return s.getErrorWithLine(fmt.Errorf("设置锁等待超时时败: %w", err))
//				}
//				// TODO: 在这里添加实际的业务逻辑
//				// 示例：更新订单状态
//				// result := tx.Model(&Order{}).
//				//     Where("order_sn = ?", orderMQParam.OrderSn).
//				//     Update("status", "processing")
//				// if result.Error != nil {
//				//     return result.Error
//				// }
//				// if result.RowsAffected == 0 {
//				//     return errors.New("订单不存在或已被处理")
//				// }
//
//				return nil // 事务成功
//			})
//
//			// 将事务中的错误传递出去
//			if txErr != nil {
//				db.Error = txErr
//			}
//			return db
//		})
//
//		// ========== 错误处理 ==========
//		if err != nil {
//			// 数据库操作失败，记录错误日志并返回错误触发重试
//			logger.ErrorWithCtx(ctx, "订单处理失败",
//				logger.String("orderSn", orderMQParam.OrderSn),
//				logger.String("source", orderMQParam.Source),
//				logger.String("busType", orderMQParam.BusType),
//				logger.Error(err))
//			return s.getErrorWithLine(fmt.Errorf("订单处理失败: %w", err))
//		}
//
//		// ========== 成功日志 ==========
//		// 记录业务处理成功的日志
//		logger.InfoWithCtx(ctx, "订单处理完成",
//			logger.String("orderSn", orderMQParam.OrderSn),
//			logger.String("source", orderMQParam.Source),
//			logger.String("busType", orderMQParam.BusType))
//
//		return nil // 成功返回
//	})
//}
