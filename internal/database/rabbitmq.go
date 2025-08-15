package database

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/rabbitmq"
)

// MqCfg 包含 RabbitMQ 配置信息
type MqCfg struct {
	ExchangeName    string        // 交换机名称
	NormalQueueName string        // 普通队列名称
	Url             string        // RabbitMQ连接URL
	DialTimeout     time.Duration // 连接超时时间
	IsDeadLetter    bool          // 是否启用死信队列
}

// RabbitMQClient 封装RabbitMQ操作
type RabbitMQClient struct {
	conn            *rabbitmq.Connection
	mqCfg           *MqCfg
	initOnce        sync.Once
	initErr         error
	producerCache   sync.Map // 缓存生产者 key: queueName
	producerOptions []rabbitmq.ProducerOption
}

// MQClientPool 连接池
var (
	clientPool sync.Map // key: configHash, value: *RabbitMQClient
)

// newRabbitMQClient 创建并初始化RabbitMQ客户端
func newRabbitMQClient(ctx context.Context, mqCfg *MqCfg) (*RabbitMQClient, error) {
	// 校验配置
	if err := validateConfig(mqCfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	client := &RabbitMQClient{
		mqCfg: mqCfg,
	}
	client.initConn(ctx) // 初始化连接

	return client, client.initErr
}

// GetClient 从连接池获取客户端
func GetClient(ctx context.Context, cfg *MqCfg) (*RabbitMQClient, error) {
	configHash := fmt.Sprintf("%s|%s|%v", cfg.ExchangeName, cfg.NormalQueueName, cfg.IsDeadLetter)
	// 尝试获取现有客户端
	if client, ok := clientPool.Load(configHash); ok {
		return client.(*RabbitMQClient), nil
	}
	// 双检查锁
	client, err := newRabbitMQClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	actual, loaded := clientPool.LoadOrStore(configHash, client)
	if loaded {
		client.CloseConnection() // 如果已存在则关闭新建的连接
	}
	return actual.(*RabbitMQClient), nil
}

// initConn 初始化RabbitMQ连接
func (c *RabbitMQClient) initConn(ctx context.Context) {
	c.initOnce.Do(func() {
		// 1. 创建连接
		c.conn, c.initErr = rabbitmq.NewConnection(
			c.mqCfg.Url,
			rabbitmq.WithLogger(logger.Get()),
			rabbitmq.WithDialTimeout(c.mqCfg.DialTimeout),
			rabbitmq.WithReconnectTime(time.Second*1),
		)
		if c.initErr != nil {
			fmt.Println("Failed to create RabbitMQ connection",
				logger.Err(c.initErr),
				interceptor.ServerCtxRequestIDField(ctx))
			return
		}

		// 2. 初始化生产者选项
		c.producerOptions = c.getProducerOptions()
	})
}

// getProducer 获取或创建生产者
func (c *RabbitMQClient) getProducer() (*rabbitmq.Producer, error) {
	if c.initErr != nil {
		return nil, fmt.Errorf("RabbitMQ client not initialized: %w", c.initErr)
	}

	// 从缓存获取
	if producer, ok := c.producerCache.Load(c.mqCfg.NormalQueueName); ok {
		return producer.(*rabbitmq.Producer), nil
	}

	// 创建新生产者
	exchangeName := c.mqCfg.ExchangeName
	normalRoutingKey := fmt.Sprintf("%s_%s", c.mqCfg.ExchangeName, c.mqCfg.NormalQueueName)
	normalExchange := rabbitmq.NewTopicExchange(exchangeName, normalRoutingKey)

	producer, err := rabbitmq.NewProducer(
		normalExchange,
		c.mqCfg.NormalQueueName,
		c.conn,
		c.producerOptions...,
	)
	if err != nil {
		return nil, err
	}

	// 缓存生产者
	c.producerCache.Store(c.mqCfg.NormalQueueName, producer)
	return producer, nil
}

// Send 发送消息到RabbitMQ
func (c *RabbitMQClient) Send(ctx context.Context, message string) error {

	producer, err := c.getProducer()
	if err != nil {
		return err
	}

	normalRoutingKey := fmt.Sprintf("%s_%s", c.mqCfg.ExchangeName, c.mqCfg.NormalQueueName)
	err = producer.PublishTopic(ctx, normalRoutingKey, []byte(message))
	if err != nil {
		// 生产失败时清除缓存
		c.producerCache.Delete(c.mqCfg.NormalQueueName)
		fmt.Println("p.PublishTopic error",
			logger.Err(err),
			interceptor.ServerCtxRequestIDField(ctx))
		return err
	}
	return nil
}

// CloseConnection 关闭连接
func (c *RabbitMQClient) CloseConnection() {
	if c.conn != nil {
		c.conn.Close()
	}
	// 清理缓存
	c.producerCache.Range(func(key, value interface{}) bool {
		if producer, ok := value.(*rabbitmq.Producer); ok {
			producer.Close()
		}
		c.producerCache.Delete(key)
		return true
	})
}

// 以下原有方法保持不变...
func (c *RabbitMQClient) getProducerOptions() []rabbitmq.ProducerOption {
	if !c.mqCfg.IsDeadLetter {
		return nil
	}
	deadLetterRoutingKey := fmt.Sprintf("%sDL", c.mqCfg.NormalQueueName)
	return []rabbitmq.ProducerOption{
		rabbitmq.WithProducerQueueDeclareOptions(
			rabbitmq.WithQueueDeclareArgs(map[string]interface{}{
				"x-dead-letter-exchange":    c.mqCfg.ExchangeName,
				"x-dead-letter-routing-key": fmt.Sprintf("%s_%s", c.mqCfg.ExchangeName, deadLetterRoutingKey),
			}),
		),
	}
}

func validateConfig(mqCfg *MqCfg) error {
	if mqCfg == nil {
		return fmt.Errorf("mq config is nil")
	}
	if mqCfg.ExchangeName == "" {
		return fmt.Errorf("exchange name cannot be empty")
	}
	if mqCfg.NormalQueueName == "" {
		return fmt.Errorf("queue name cannot be empty")
	}
	if mqCfg.Url == "" {
		return fmt.Errorf("url cannot be empty")
	}
	if !isValidAMQPUrl(mqCfg.Url) {
		return fmt.Errorf("invalid AMQP URL format, must be like amqp://user:pass@host:port/vhost")
	}
	if mqCfg.DialTimeout < 0 {
		return fmt.Errorf("dial timeout must be non-negative")
	}
	if mqCfg.DialTimeout > 0 && mqCfg.DialTimeout < time.Second {
		return fmt.Errorf("dial timeout too small, must be >= 1s")
	}
	return nil
}

func isValidAMQPUrl(url string) bool {
	amqpURLRegex := regexp.MustCompile(`^amqp[s]?://[^:]+:[^@]+@[^:/]+(:\d+)?(/[^\s]*)?$`)
	return amqpURLRegex.MatchString(url)
}
