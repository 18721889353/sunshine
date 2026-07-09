package consumers

//
//import (
//	"context"
//	"encoding/json"
//	"errors"
//	"fmt"
//	"github.com/18721889353/sunshine/internal/config"
//	"github.com/18721889353/sunshine/internal/consts"
//	"github.com/18721889353/sunshine/internal/database"
//	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"
//	"slices"
//	"strings"
//
//	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq/gorabbitmqconsumer"
//	"github.com/18721889353/sunshine/pkg/logger"
//	"github.com/spf13/cast"
//)
//
//func init() {
//	// 实例化对象
//	oc := &orderConsumer{}
//	oc.BaseMQConsumer.BaseConsumer = gorabbitmqconsumer.NewBaseConsumer(
//		"doingOrderMqConsumer",
//		oc.HandleMessage,
//	)
//	// 注册到全局注册表 (mq.go 中的 registry)
//	mq.RegisterConsumer(oc)
//}
//
//// 订单消息入参
//
//// OrderMQParam 订单消息参数
//type OrderMQParam struct {
//	Source  string `json:"source"`
//	BusType string `json:"bus_type"`
//	OrderSn string `json:"order_sn"`
//}
//
//// UnmarshalJSON 自定义反序列化，兼容上游 PHP 可能传 string 或 number 混合类型
//func (p *OrderMQParam) UnmarshalJSON(data []byte) error {
//	raw := make(map[string]any)
//	if err := json.Unmarshal(data, &raw); err != nil {
//		return err
//	}
//	p.Source = cast.ToString(raw["source"])
//	p.BusType = cast.ToString(raw["bus_type"])
//	p.OrderSn = cast.ToString(raw["order_sn"])
//	return nil
//}
//
//// Validate 验证数据不能为空
//func (p *OrderMQParam) Validate() error {
//	if p.Source == "" {
//		return errors.New("source 不能为空")
//	}
//	if p.BusType == "" {
//		return errors.New("bus_type 不能为空")
//	}
//	if p.OrderSn == "" {
//		return errors.New("order_sn 不能为空")
//	}
//	return nil
//}
//
//type orderConsumer struct {
//	BaseMQConsumer
//}
//
//// Start 启动消费者
//func (s *orderConsumer) Start(ctx context.Context) error {
//	// 初始化基础MQ消费者（包含所有DAO和cache，使用sync.Once确保只初始化一次）
//	s.initBaseMQConsumer()
//	// 设置自定义重试策略
//	s.retryStrategyFunc = func(ctx context.Context, orderSn, busType string, currentErr error, maxErrorNum int64) error {
//		return s.customRetryStrategy(ctx, orderSn, busType, currentErr)
//	}
//
//	// 1. 从 database 获取连接
//	conn, err := database.GetMainRabbitMQ().GetConnection(ctx)
//	if err != nil {
//		return fmt.Errorf(s.Name()+":get connection error: %w", err)
//	}
//	// 2. 【新增】设置连接释放回调，goroutine退出时自动归还连接到连接池
//	s.BaseMQConsumer.BaseConsumer.SetConnRelease(func() {
//		if putErr := database.GetMainRabbitMQ().PutConnection(context.Background(), conn); putErr != nil {
//			logger.WarnWithCtx(context.Background(), "put connection error", logger.Err(putErr))
//		}
//	})
//	// 3. 调用公共包的 Start
//	return s.BaseMQConsumer.BaseConsumer.Start(ctx, conn, config.Get().Rabbitmq.DoingOrder)
//}
//
//// customRetryStrategy 自定义重试策略（普通订单消费者专用）
//func (s *orderConsumer) customRetryStrategy(ctx context.Context, orderSn, busType string, currentErr error) error {
//	return s.trace(ctx, s.Name()+":customRetryStrategy", func() error {
//		if orderSn == "" || currentErr == nil {
//			return currentErr
//		}
//		// 余额不足,不重试
//		errMsg := currentErr.Error()
//
//		shouldStopRetry := slices.ContainsFunc(
//			[]string{"test"},
//			func(keyword string) bool {
//				return strings.Contains(errMsg, keyword)
//			},
//		)
//		if shouldStopRetry {
//			logger.InfoWithCtx(ctx, errMsg+",不重试", logger.Err(currentErr))
//			return nil
//		}
//		errorNum, successNum, err := s.BaseMQConsumer.getMqLogNum(ctx, orderSn)
//		if err != nil {
//			logger.WarnWithCtx(ctx, "获取MQ日志数量失败", logger.Err(err))
//			return currentErr
//		}
//		// 如果历史上已经成功过，本次错误不再重试
//		if successNum >= 1 {
//			return nil
//		}
//		// 如果错误次数超过阈值，记录警告并停止重试（Ack）
//		if errorNum > consts.MqMaxRetryErrorNum {
//			logger.WarnWithCtx(ctx, "错误次数过多，停止重试", logger.Err(currentErr))
//			return nil
//		}
//		return currentErr
//	},
//		logger.String("remark", "自定义重试策略(余额不足检查+重试次数检查)"),
//		logger.String("order_sn", orderSn),
//		logger.String("bus_type", busType),
//	)
//}
//
//// HandleMessage 处理订单消息
//func (s *orderConsumer) HandleMessage(ctx context.Context, data []byte, messageId, _ string) error {
//	// 预解析消息数据（只解析一次）
//	var cachedParam *OrderMQParam
//	var parseErr error
//
//	return s.HandleMessageTemplate(
//		ctx,
//		data,
//		messageId,
//		// 轻量级提取 orderSn（复用预解析的数据）
//		func(data []byte) (orderSn string, busType string, err error) {
//			if cachedParam == nil && parseErr == nil {
//				cachedParam = &OrderMQParam{}
//				parseErr = json.Unmarshal(data, cachedParam)
//			}
//			if parseErr != nil {
//				return consts.MqUnknownOrderSn, "", s.getErrorWithLine(parseErr)
//			}
//			return cachedParam.OrderSn, cachedParam.BusType, nil
//		},
//		// 完整解析并执行业务逻辑（复用预解析的数据）
//		func(ctx context.Context, data []byte) (orderSn string, busType string, err error) {
//			// 如果尚未解析，则解析
//			if cachedParam == nil && parseErr == nil {
//				cachedParam = &OrderMQParam{}
//				parseErr = json.Unmarshal(data, cachedParam)
//			}
//			if parseErr != nil {
//				return consts.MqUnknownOrderSn, "", s.getErrorWithLine(parseErr)
//			}
//
//			orderSn = cachedParam.OrderSn
//			busType = cachedParam.BusType
//
//			// 验证参数
//			if err = cachedParam.Validate(); err != nil {
//				return orderSn, busType, s.getErrorWithLine(err)
//			}
//
//			// 执行核心业务逻辑
//			if err = s.logic(ctx, cachedParam); err != nil {
//				return orderSn, busType, s.getErrorWithLine(err)
//			}
//			return orderSn, busType, nil
//		},
//		true, // 使用分布式锁
//		consts.MqMaxRetryErrorNum,
//	)
//}
//
//func (s *orderConsumer) logic(ctx context.Context, orderMQParam *OrderMQParam) error {
//	if orderMQParam == nil {
//		return s.getErrorWithLine(errors.New("orderMQParam 不能为空"))
//	}
//	return s.trace(ctx, s.Name()+":logic", func() error {
//		return nil
//	},
//		logger.String("remark", "普通订单处理(幂等性检查+订单验证+余额检查+发送余额扣减MQ)"),
//		logger.String("order_sn", orderMQParam.OrderSn),
//		logger.String("bus_type", orderMQParam.BusType),
//		logger.String("source", orderMQParam.Source),
//	)
//}
