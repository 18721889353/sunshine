package consumers

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/database"
	mq "github.com/18721889353/sunshine/internal/mq/rabbitmq"
	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq/gorabbitmqConsumer"
	"github.com/18721889353/sunshine/pkg/logger"
)

// orderConsumer 结构体
type orderConsumer struct {
	*gorabbitmqConsumer.BaseConsumer
}

// init 函数：程序启动时自动执行，将消费者注册到全局中心
func init() {
	// 实例化对象
	oc := &orderConsumer{}
	oc.BaseConsumer = gorabbitmqConsumer.NewBaseConsumer(
		"doingOrderMqConsumer",
		oc.handleMessage,
	)
	// 注册到全局注册表 (mq.go 中的 registry)
	mq.RegisterConsumer(oc)
}

// Start 消费者启动入口
func (s *orderConsumer) Start(ctx context.Context) error {
	// 1. 从 database 获取连接
	conn, err := database.GetMainRabbitMQ().GetConnection(ctx)
	if err != nil {
		return fmt.Errorf(s.Name()+" get connection error: %w", err)
	}
	// 2. 调用公共包的 Start
	return s.BaseConsumer.Start(ctx, conn, config.Get().Rabbitmq.DoingOrder)
}
func (s *orderConsumer) getErrorWithLine(err error, params ...map[string]any) error {
	if err != nil {
		_, file, line, ok := runtime.Caller(1) // 1 表示上一层调用者
		if ok {
			return fmt.Errorf("%s:%d: %v,%w", file, line, params, err)
		}
	}
	return err
}

// handleMessage 具体的业务逻辑处理
func (s *orderConsumer) handleMessage(ctx context.Context, data []byte, messageId, tagID string) (err error) {
	ctx = context.WithValue(ctx, logger.ContextKeyRequestID, messageId)
	defer func() {
		if r := recover(); r != nil {
			//使用 debug.Stack() 获取堆栈信息并保持原始格式
			logger.WarnWithCtx(ctx,
				fmt.Sprintf(s.Name()+" panic recovered: %v\nstack: %s", r, string(debug.Stack())),
			)
			err = fmt.Errorf("panic recovered: %v\n", r)
		}
	}()
	return nil
}
