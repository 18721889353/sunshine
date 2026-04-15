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
	"github.com/18721889353/sunshine/pkg/goredis"
	"github.com/18721889353/sunshine/pkg/logger"
	pkgtracer "github.com/18721889353/sunshine/pkg/tracer"
	"gorm.io/gorm"
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

	// 1. 初始化配置（雪花 ID + MySQL + Redis 需要）
	config.Set(&config.Config{
		App: config.App{
			MachineID:   1,
			EnableTrace: true, // 启用链路追踪
		},
		Database: config.Database{
			Mysql: config.Mysql{
				Dsn:                  "root:jianguo123@(43.143.78.234:3306)/helllo?parseTime=true&loc=UTC&charset=utf8mb4",
				EnableLog:            true,
				SlowQueryThresholdMs: 100,
				MaxIdleConns:         10,
				MaxOpenConns:         20,
				ConnMaxLifetime:      30,
				MaxIdleTime:          5,
			},
		},
		Redis: config.Redis{
			Dsn:          ":jianguo123@127.0.0.1:6379/0",
			DialTimeout:  10,
			ReadTimeout:  2,
			WriteTimeout: 2,
			PoolSize:     10,
			MinIdleConns: 5,
			MaxConnAge:   60,
			PoolTimeout:  10,
			IdleTimeout:  30,
		},
	})

	// 2. 初始化基础设施（在业务 Span 创建之前完成，避免追踪初始化 Span）
	database.InitSnowNode()
	logger.InfoWithCtx(ctx, "[snowflake] initialized")

	// 3. 初始化 OpenTelemetry Tracer
	pkgtracer.InitWithConfig(
		serviceName,
		env,
		version,
		"",             // 不使用 Agent 模式
		"",             // 不使用 Agent 模式
		samplingRate,   // 全量采样
		aliyunEndpoint, // 阿里云 OTLP endpoint
	)
	logger.InfoWithCtx(ctx, "[tracer] was initialized")

	// 4. 初始化 MySQL（此时无业务 Span，初始化 Span 会独立上报）
	mysqlDB := database.InitMysql()
	logger.InfoWithCtx(ctx, "[mysql] initialized")

	// 5. 初始化 Redis（此时无业务 Span，初始化 Span 会独立上报）
	database.InitRedis()
	redisCli := database.GetRedisCli()
	logger.InfoWithCtx(ctx, "[redis] initialized")

	// 预热 Redis 连接（在业务 Span 之前完成连接建立和认证）
	if err := redisCli.Ping(ctx).Err(); err != nil {
		logger.ErrorWithCtx(ctx, "Redis ping failed: "+err.Error())
	}
	logger.InfoWithCtx(ctx, "[redis] connection warmed up")

	// 6. 创建业务 Root Span（此时基础设施已就绪，只追踪业务操作）
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

	// 5. 在业务 Span 内执行 Redis 测试（验证 request_id 传递）
	fmt.Println("\n📝 Testing Redis operations...")
	testRedisOps(rootCtx, redisCli, reqID)

	// 6. 在业务 Span 内执行 MySQL 测试（验证 request_id 传递）
	fmt.Println("\n📝 Testing MySQL operations...")
	testMySQLOps(rootCtx, mysqlDB, reqID)

	// 5. 在业务 Span 内创建 RabbitMQ 连接（使其成为子 Span）
	conn, err := gorabbitmq.NewConnection(
		rootCtx,
		rabbitMQURL,
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

	// 6. 创建 Producer
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

	// 7. 创建 Consumer
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

	// 启动消费者监听（在 Consumer 回调中不执行额外操作，保持链路清晰）
	msgReceived := make(chan string, 1)
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

	// 8. 发送消息 (Producer 会自动从 rootCtx 提取 Trace Context 并注入 Header)
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

	// 关闭数据库连接
	if err := database.CloseRedis(); err != nil {
		log.Printf("Error closing Redis: %v", err)
	}
	if sqlDB, err := mysqlDB.DB(); err == nil {
		_ = sqlDB.Close()
	}

	// 关闭 Tracer（确保所有 Span 都被上报）
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pkgtracer.Close(ctx); err != nil {
		log.Printf("Error closing tracer: %v", err)
	}
}

// testRedisOps 测试 Redis 操作（验证 request_id 传递）
func testRedisOps(ctx context.Context, redisCli *goredis.Client, reqID string) {
	testKey := fmt.Sprintf("trace:test:%s", reqID)
	testValue := fmt.Sprintf("Redis test value at %d", time.Now().Unix())

	// SET 操作
	if err := redisCli.Set(ctx, testKey, testValue, 5*time.Minute).Err(); err != nil {
		fmt.Printf("❌ Redis SET failed: %v\n", err)
		return
	}
	fmt.Printf("✅ Redis SET: %s = %s\n", testKey, testValue)

	// GET 操作
	val, err := redisCli.Get(ctx, testKey).Result()
	if err != nil {
		fmt.Printf("❌ Redis GET failed: %v\n", err)
		return
	}
	fmt.Printf("✅ Redis GET: %s = %s\n", testKey, val)

	// DEL 操作
	if err := redisCli.Del(ctx, testKey).Err(); err != nil {
		fmt.Printf("❌ Redis DEL failed: %v\n", err)
		return
	}
	fmt.Printf("✅ Redis DEL: %s\n", testKey)
}

// testMySQLOps 测试 MySQL 操作（验证 request_id 传递）
func testMySQLOps(ctx context.Context, db *gorm.DB, reqID string) {
	// 执行一个简单的 SELECT 查询
	selectSQL := "SELECT @@version as version, DATABASE() as current_db"
	
	var version, currentDB string
	if err := db.Raw(selectSQL).Scan(&struct {
		Version   *string
		CurrentDB *string
	}{&version, &currentDB}).Error; err != nil {
		fmt.Printf("❌ MySQL SELECT failed: %v\n", err)
		return
	}
	fmt.Printf("✅ MySQL SELECT: version=%s, database=%s\n", version, currentDB)

	// 查询 cp_dealer 表的记录数
	countSQL := "SELECT COUNT(*) as cnt FROM cp_dealer LIMIT 1"
	var count int
	if err := db.Raw(countSQL).Scan(&count).Error; err != nil {
		fmt.Printf("⚠️  MySQL cp_dealer table query (expected if table doesn't exist): %v\n", err)
		fmt.Println("💡 Tip: You can create a test table to verify full trace functionality")
		return
	}
	fmt.Printf("✅ MySQL cp_dealer: found %d records\n", count)
}
