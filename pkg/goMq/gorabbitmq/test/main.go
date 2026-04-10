package main

import (
	"context"
	"fmt"
	"github.com/18721889353/sunshine/internal/config"
	"github.com/18721889353/sunshine/internal/database"
	"log"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	"github.com/18721889353/sunshine/pkg/logger"
	pkgtracer "github.com/18721889353/sunshine/pkg/tracer"
)

// 阿里云 Tracing 配置
const (
	serviceName    = "rabbitmq-trace-test"
	env            = "test"
	version        = "1.0.0"
	rabbitMQURL    = "amqp://sunjianguo:jianguo123@43.143.78.234:5672/"
	samplingRate   = 1.0 // 全量采样
	aliyunEndpoint = "http://tracing-analysis-dc-sh.aliyuncs.com/adapt_j1hwr1hssi@6205c493199393c_j1hwr1hssi@53df7ad2afe8301/api/traces"
)

func main() {
	ctx := context.Background()

	// 1. 初始化配置（雪花 ID 需要）
	// 测试环境：直接设置 MachineID，跳过配置文件
	config.Set(&config.Config{
		App: config.App{
			MachineID: 1,
		},
	})
	database.InitSnowNode()

	// 2. 初始化 OpenTelemetry Tracer（使用项目标准的 tracer.InitWithConfig）
	pkgtracer.InitWithConfig(
		serviceName,
		env,
		version,
		"",             // 不使用 Agent 模式
		"",             // 不使用 Agent 模式
		samplingRate,   // 全量采样
		aliyunEndpoint, // 阿里云 OTLP endpoint
	)
	logger.Info("[tracer] was initialized")

	// 3. 先创建业务 Root Span（确保它成为整个链路的起点）
	reqID := database.GetSnowId().String()
	fmt.Printf("\n🔗 Starting trace with RequestID: %s\n", reqID)

	tracer := otel.Tracer(serviceName)
	rootCtx, rootSpan := tracer.Start(ctx, "mock-http-request", trace.WithAttributes(
		semconv.HTTPRequestMethodKey.String("GET"),
		semconv.URLPath("/api/test"),
	))
	rootSpan.SetAttributes(attribute.String("request_id", reqID))

	// 将 request_id 注入 Context
	rootCtx = context.WithValue(rootCtx, "request_id", reqID)

	// 4. 在业务 Span 内创建 RabbitMQ 连接（使其成为子 Span）
	conn, err := gorabbitmq.NewConnection(
		rootCtx,
		rabbitMQURL,
		gorabbitmq.WithLogger(logger.Get()),
		gorabbitmq.WithMaxRetries(0),
		gorabbitmq.WithReconnectTime(2*time.Second),
		gorabbitmq.WithDialTimeout(5*time.Second),
	)
	if err != nil {
		log.Fatalf("create connection failed: %v", err)
	}
	defer conn.Close()

	exchangeName := "test_trace_exchange"
	queueName := "test_trace_queue"
	routingKey := "test.trace.key"

	exchange := gorabbitmq.NewDirectExchange(exchangeName, routingKey)

	// 4. 创建 Producer
	producerOpts := []gorabbitmq.ProducerOption{
		gorabbitmq.WithProducerNormalLetterOptions(
			gorabbitmq.WithNormalLetter(exchange.Name(), queueName, exchange.RoutingKey()),
			gorabbitmq.WithNormalLetterExchangeDeclareOptions(
				gorabbitmq.WithExchangeDeclareDurable(true),
			),
			gorabbitmq.WithNormalLetterNormalQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(true),
			),
		),
	}
	producer, err := gorabbitmq.NewProducer(rootCtx, exchange, conn, producerOpts...)
	if err != nil {
		log.Fatalf("create producer failed: %v", err)
	}
	defer producer.Close()

	// 5. 创建 Consumer
	consumerOpts := []gorabbitmq.ConsumerOption{
		gorabbitmq.WithConsumerNormalLetterOptions(
			gorabbitmq.WithNormalLetter(exchange.Name(), queueName, exchange.RoutingKey()),
			gorabbitmq.WithNormalLetterExchangeDeclareOptions(
				gorabbitmq.WithExchangeDeclareDurable(true),
			),
			gorabbitmq.WithNormalLetterNormalQueueDeclareOptions(
				gorabbitmq.WithQueueDeclareDurable(true),
			),
		),
		gorabbitmq.WithConsumerAutoAck(true),
	}
	consumer, err := gorabbitmq.NewConsumer(exchange, queueName, conn, consumerOpts...)
	if err != nil {
		log.Fatalf("create consumer failed: %v", err)
	}

	// 启动消费者监听
	msgReceived := make(chan string, 1) // 用于等待消息被消费
	go func() {
		fmt.Println("🚀 Consumer started...")
		consumer.Consume(rootCtx, func(ctx context.Context, data []byte, msgID, tagID string) error {
			fmt.Printf("✅ Received message: %s, ID: %s\n", string(data), msgID)
			// 通知主 goroutine 消息已被消费
			select {
			case msgReceived <- msgID:
			default:
			}
			return nil
		})
	}()

	time.Sleep(2 * time.Second) // 等待消费者就绪

	// 6. 发送消息 (Producer 会自动从 rootCtx 提取 Trace Context 并注入 Header)
	msgBody := fmt.Sprintf("Hello from trace test at %d", time.Now().Unix())
	err = producer.PublishDirect(rootCtx, routingKey, []byte(msgBody), uuid.New().String())
	if err != nil {
		log.Fatalf("publish message failed: %v", err)
	}
	fmt.Println("📤 Message published")

	// 等待消费完成（确保 consume span 被完整记录，并建立父子关系）
	select {
	case msgID := <-msgReceived:
		fmt.Printf("🏁 Message consumed successfully: %s\n", msgID)
	case <-time.After(10 * time.Second):
		fmt.Println("⏱️ Timeout waiting for message")
	}

	// 在所有子 Span 完成后，再结束 Root Span
	rootSpan.End()

	// 关闭 Tracer（确保所有 Span 都被上报）
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pkgtracer.Close(ctx); err != nil {
		log.Printf("Error closing tracer: %v", err)
	}
}
