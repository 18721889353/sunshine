package gorabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

// Producer RabbitMQ 生产者结构体
type Producer struct {
	conn       *Connection         // RabbitMQ 连接
	channel    *amqp091.Channel    // AMQP 通道
	queue      amqp091.Queue       // 队列
	exchange   string              // 交换机名称
	routingKey string              // 路由键
	mu         sync.RWMutex        // 读写锁
	config     ProducerConfig      // 生产者配置
}

// ProducerConfig RabbitMQ 生产者配置
type ProducerConfig struct {
	Exchange     string          // 交换机名称
	ExchangeType string          // 交换机类型
	QueueName    string          // 队列名称
	RoutingKey   string          // 路由键
	Durable      bool            // 是否持久化
	AutoDelete   bool            // 是否自动删除
	Exclusive    bool            // 是否独占
	NoWait       bool            // 是否非阻塞
	Args         amqp091.Table   // 其他参数
}

// Message 消息结构体
type Message struct {
	ID        string      `json:"id"`        // 消息ID
	Timestamp time.Time   `json:"timestamp"` // 时间戳
	Body      interface{} `json:"body"`      // 消息体
}

// DelayedMessage 延迟消息结构体
type DelayedMessage struct {
	Message
	Delay time.Duration `json:"delay"` // 延迟时间
}

// NewProducer 创建新的生产者实例
func NewProducer(conn *Connection, config ProducerConfig) (*Producer, error) {
	producer := &Producer{
		conn:   conn,
		config: config,
	}

	if err := producer.init(); err != nil {
		return nil, fmt.Errorf("failed to initialize producer: %w", err)
	}

	return producer, nil
}

// init 初始化生产者
func (p *Producer) init() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.conn.CheckConnected() {
		return fmt.Errorf("rabbitmq connection is not available")
	}

	amqpConn := p.conn.GetConn()
	if amqpConn == nil {
		return fmt.Errorf("failed to get amqp connection")
	}

	var err error
	// 创建通道
	p.channel, err = amqpConn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open a channel: %w", err)
	}

	// 声明交换机
	if p.config.Exchange != "" {
		err = p.channel.ExchangeDeclare(
			p.config.Exchange,
			p.config.ExchangeType,
			p.config.Durable,
			p.config.AutoDelete,
			false, // internal
			p.config.NoWait,
			p.config.Args,
		)
		if err != nil {
			return fmt.Errorf("failed to declare exchange: %w", err)
		}
		p.exchange = p.config.Exchange
	}

	// 声明队列
	p.queue, err = p.channel.QueueDeclare(
		p.config.QueueName,
		p.config.Durable,
		p.config.AutoDelete,
		p.config.Exclusive,
		p.config.NoWait,
		p.config.Args,
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// 绑定队列到交换机（如果指定了交换机）
	if p.config.Exchange != "" && p.config.RoutingKey != "" {
		err = p.channel.QueueBind(
			p.queue.Name,
			p.config.RoutingKey,
			p.config.Exchange,
			p.config.NoWait,
			p.config.Args,
		)
		if err != nil {
			return fmt.Errorf("failed to bind queue: %w", err)
		}
		p.routingKey = p.config.RoutingKey
	}

	return nil
}

// Publish 发布消息
func (p *Producer) Publish(ctx context.Context, body interface{}) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.conn.CheckConnected() {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	if p.channel == nil {
		return fmt.Errorf("channel is not available")
	}

	// 构造消息
	msg := Message{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Body:      body,
	}

	// 序列化消息
	jsonBody, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// 发布消息
	err = p.channel.PublishWithContext(
		ctx,
		p.exchange,
		p.routingKey,
		false, // mandatory
		false, // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Body:        jsonBody,
			Timestamp:   time.Now(),
			MessageId:   msg.ID,
		})

	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// PublishWithTTL 发布带有 TTL 的消息
func (p *Producer) PublishWithTTL(ctx context.Context, body interface{}, ttl time.Duration) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.conn.CheckConnected() {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	if p.channel == nil {
		return fmt.Errorf("channel is not available")
	}

	// 构造消息
	msg := Message{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Body:      body,
	}

	// 序列化消息
	jsonBody, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// 发布带有 TTL 的消息
	err = p.channel.PublishWithContext(
		ctx,
		p.exchange,
		p.routingKey,
		false, // mandatory
		false, // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Body:        jsonBody,
			Timestamp:   time.Now(),
			MessageId:   msg.ID,
			Expiration:  fmt.Sprintf("%d", ttl.Milliseconds()), // 设置消息 TTL（毫秒）
		})

	if err != nil {
		return fmt.Errorf("failed to publish message with TTL: %w", err)
	}

	return nil
}

// PublishDelayed 发布延迟消息
// 注意：需要安装 RabbitMQ 延迟消息插件（rabbitmq-delayed-message-exchange）
func (p *Producer) PublishDelayed(ctx context.Context, body interface{}, delay time.Duration) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.conn.CheckConnected() {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	if p.channel == nil {
		return fmt.Errorf("channel is not available")
	}

	// 构造延迟消息
	delayedMsg := DelayedMessage{
		Message: Message{
			ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
			Timestamp: time.Now(),
			Body:      body,
		},
		Delay: delay,
	}

	// 序列化消息
	jsonBody, err := json.Marshal(delayedMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal delayed message: %w", err)
	}

	// 发布延迟消息
	headers := amqp091.Table{
		"x-delay": int64(delay / time.Millisecond), // 延迟时间（毫秒）
	}

	err = p.channel.PublishWithContext(
		ctx,
		p.exchange,
		p.routingKey,
		false, // mandatory
		false, // immediate
		amqp091.Publishing{
			ContentType: "application/json",
			Body:        jsonBody,
			Timestamp:   time.Now(),
			MessageId:   delayedMsg.ID,
			Headers:     headers,
		})

	if err != nil {
		return fmt.Errorf("failed to publish delayed message: %w", err)
	}

	return nil
}

// PublishRaw 发布原始消息（不包装）
func (p *Producer) PublishRaw(ctx context.Context, contentType string, body []byte) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.conn.CheckConnected() {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	if p.channel == nil {
		return fmt.Errorf("channel is not available")
	}

	// 发布消息
	err := p.channel.PublishWithContext(
		ctx,
		p.exchange,
		p.routingKey,
		false, // mandatory
		false, // immediate
		amqp091.Publishing{
			ContentType: contentType,
			Body:        body,
			Timestamp:   time.Now(),
			MessageId:   fmt.Sprintf("%d", time.Now().UnixNano()),
		})

	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// PublishRawWithTTL 发布带有 TTL 的原始消息
func (p *Producer) PublishRawWithTTL(ctx context.Context, contentType string, body []byte, ttl time.Duration) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.conn.CheckConnected() {
		return fmt.Errorf("not connected to RabbitMQ")
	}

	if p.channel == nil {
		return fmt.Errorf("channel is not available")
	}

	// 发布带有 TTL 的消息
	err := p.channel.PublishWithContext(
		ctx,
		p.exchange,
		p.routingKey,
		false, // mandatory
		false, // immediate
		amqp091.Publishing{
			ContentType: contentType,
			Body:        body,
			Timestamp:   time.Now(),
			MessageId:   fmt.Sprintf("%d", time.Now().UnixNano()),
			Expiration:  fmt.Sprintf("%d", ttl.Milliseconds()), // 设置消息 TTL（毫秒）
		})

	if err != nil {
		return fmt.Errorf("failed to publish message with TTL: %w", err)
	}

	return nil
}

// Close 关闭生产者
func (p *Producer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.channel != nil {
		if err := p.channel.Close(); err != nil {
			log.Printf("Error closing channel: %v", err)
		}
	}

	return nil
}

// IsConnected 检查是否已连接
func (p *Producer) IsConnected() bool {
	return p.conn.CheckConnected()
}

// GetQueueInfo 获取队列信息
func (p *Producer) GetQueueInfo() amqp091.Queue {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.queue
}