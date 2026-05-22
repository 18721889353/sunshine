// Package consumers  测试案例
package consumers

//import (
//	"context"
//	"encoding/json"
//	"errors"
//	"fmt"
//	"github.com/18721889353/sunshine/internal/config"
//	"github.com/18721889353/sunshine/internal/consts"
//	"github.com/18721889353/sunshine/internal/database"
//	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"
//	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq/gorabbitmqconsumer"
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
//// Validate 验证数据不能为空
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
//type orderConsumer struct {
//	BaseMQConsumer
//}
//
//// Start 启动消费者
//func (s *orderConsumer) Start(ctx context.Context) error {
//	// 初始化基础MQ消费者（包含所有DAO和cache，使用sync.Once确保只初始化一次）
//	s.initBaseMQConsumer()
//	// 1. 从 database 获取连接
//	conn, err := database.GetMainRabbitMQ().GetConnection(ctx)
//	if err != nil {
//		return fmt.Errorf(s.Name()+":get connection error: %w", err)
//	}
//	// 2. 调用公共包的 Start
//	return s.BaseMQConsumer.BaseConsumer.Start(ctx, conn, config.Get().Rabbitmq.DoingOrder)
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
//func (s *orderConsumer) logic(ctx context.Context, orderMQParam *OrderMQParam) error {
//	return s.trace(ctx, s.Name()+":logic", func() error {
//		return nil
//	})
//}
