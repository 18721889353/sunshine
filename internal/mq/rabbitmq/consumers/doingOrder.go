package consumers

//
//import (
//	"context"
//	"fmt"
//)
//
//func init() {
//	mq.RegisterConsumer(newOrderConsumer())
//}
//
//type orderConsumer struct {
//	*mq.BaseConsumer
//}
//
//// NewOrderConsumer 初始化订单消费者
//func newOrderConsumer() *orderConsumer {
//	return &orderConsumer{
//		BaseConsumer: mq.NewBaseConsumer("doingOrderMqConsumer", handleOrderMessage),
//	}
//}
//
//// Start 启动消费者
//func (s *orderConsumer) Start() error {
//	cfg := config.Get().Rabbitmq
//	return s.BaseConsumer.Start(cfg.DoingOrder, cfg.DoingOrder.QueueType)
//}
//
//// handleOrderMessage 处理订单消息
//func handleOrderMessage(ctx context.Context, data []byte, tagID string) error {
//	// 解析订单数据
//	fmt.Println(string(data), tagID, "111111111111111111111111")
//	// 在这里添加实际的订单处理逻辑
//	return nil
//}
