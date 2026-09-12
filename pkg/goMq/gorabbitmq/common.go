// Package gorabbitmq 提供 RabbitMQ 消息队列的高性能客户端实现。
package gorabbitmq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ErrClosed closed
var ErrClosed = amqp.ErrClosed

const (
	// defaultExchangeName 默认交换机名称(表示未配置自定义交换机)
	defaultExchangeName = "sunshine"

	exchangeTypeDirect         = "direct"            // exchangeTypeDirect 直连交换机类型
	exchangeTypeTopic          = "topic"             // exchangeTypeTopic 主题交换机类型
	exchangeTypeFanout         = "fanout"            // exchangeTypeFanout 广播交换机类型
	exchangeTypeHeaders        = "headers"           // exchangeTypeHeaders 头交换机类型
	exchangeTypeDelayedMessage = "x-delayed-message" // exchangeTypeDelayedMessage 延迟消息交换机类型

	// HeadersTypeAll all
	HeadersTypeAll HeadersType = "all" // HeadersTypeAll 匹配headers中所有键值对
	// HeadersTypeAny any
	HeadersTypeAny HeadersType = "any" // HeadersTypeAny 匹配headers中任意键值对
)

// HeadersType 头交换机匹配类型
type HeadersType = string

// Exchange RabbitMQ交换机最小管理单元
type Exchange struct {
	name        string                 // name 交换机名称
	eType       string                 // eType 交换机类型: direct, topic, fanout, headers, x-delayed-message
	routingKey  string                 // routingKey 路由键
	headersKeys map[string]interface{} // headersKeys 头交换机的键值对配置
}

// Name 返回交换机名称
func (e *Exchange) Name() string {
	return e.name
}

// Type 返回交换机类型
func (e *Exchange) Type() string {
	return e.eType
}

// RoutingKey 返回路由键
func (e *Exchange) RoutingKey() string {
	return e.routingKey
}

// HeadersKeys 返回头交换机键值对
func (e *Exchange) HeadersKeys() map[string]interface{} {
	return e.headersKeys
}

// Validate 校验交换机配置合法性
func (e *Exchange) Validate() error {
	if e.name == "" {
		return fmt.Errorf("exchange name cannot be empty")
	}
	switch e.eType {
	case exchangeTypeDirect, exchangeTypeTopic, exchangeTypeFanout, exchangeTypeHeaders, exchangeTypeDelayedMessage:
		return nil
	default:
		return fmt.Errorf("unsupported exchange type: %s (supported: direct, topic, fanout, headers, x-delayed-message)", e.eType)
	}
}

// NewDirectExchange create a direct exchange
func NewDirectExchange(exchangeName string, routingKey string) *Exchange {
	if exchangeName == "" || routingKey == "" {
		// 处理 exchangeName 为空的情况
		panic("exchangeName or routingKey cannot be empty")
	}
	return &Exchange{
		name:       exchangeName,
		eType:      exchangeTypeDirect,
		routingKey: routingKey,
	}
}

// NewTopicExchange create a topic exchange
func NewTopicExchange(exchangeName string, routingKey string) *Exchange {
	if exchangeName == "" || routingKey == "" {
		// 处理 exchangeName 为空的情况
		panic("exchangeName or routingKey cannot be empty")
	}
	return &Exchange{
		name:       exchangeName,
		eType:      exchangeTypeTopic,
		routingKey: routingKey,
	}
}

// NewFanoutExchange create a fanout exchange
func NewFanoutExchange(exchangeName string) *Exchange {
	if exchangeName == "" {
		// 处理 exchangeName 为空的情况
		panic("exchangeName  cannot be empty")
	}
	return &Exchange{
		name:       exchangeName,
		eType:      exchangeTypeFanout,
		routingKey: "",
	}
}

// NewHeadersExchange create a headers exchange, the headerType supports "all" and "any"
func NewHeadersExchange(exchangeName string, headersType HeadersType, keys map[string]interface{}) *Exchange {
	if exchangeName == "" {
		// 处理 exchangeName 为空的情况
		panic("exchangeName  cannot be empty")
	}
	if keys == nil {
		keys = make(map[string]interface{})
	}

	switch headersType {
	case HeadersTypeAll, HeadersTypeAny:
		keys["x-match"] = headersType
	default:
		keys["x-match"] = HeadersTypeAll
	}

	return &Exchange{
		name:        exchangeName,
		eType:       exchangeTypeHeaders,
		routingKey:  "",
		headersKeys: keys,
	}
}

// NewDelayedMessageExchange create a delayed message exchange
func NewDelayedMessageExchange(exchangeName string, e *Exchange) *Exchange {
	if exchangeName == "" {
		// 处理 exchangeName 为空的情况
		panic("exchangeName  cannot be empty")
	}
	return &Exchange{
		name:        exchangeName,
		eType:       exchangeTypeDelayedMessage,
		routingKey:  e.routingKey,
		headersKeys: e.headersKeys,
	}
}

// -------------------------------------------------------------------------------------------

// ExchangeDeclareOption 交换机声明选项函数类型
type ExchangeDeclareOption func(*exchangeDeclareOptions)

type exchangeDeclareOptions struct {
	durable    bool       // durable 交换机是否持久化，即使服务器重启也保留
	autoDelete bool       // autoDelete 是否自动删除，当最后一个消费者断开连接时是否自动删除
	internal   bool       // internal 是否为内部使用，true表示只用于exchange到exchange的绑定
	noWait     bool       // noWait 是否非阻塞处理，true表示不等待服务器确认
	args       amqp.Table // args 交换机的其他属性参数
}

func (o *exchangeDeclareOptions) apply(opts ...ExchangeDeclareOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultExchangeDeclareOptions 默认交换机声明设置
func defaultExchangeDeclareOptions() *exchangeDeclareOptions {
	return &exchangeDeclareOptions{
		durable:    true,  // 默认持久化
		autoDelete: false, // 默认不自动删除
		internal:   false, // 默认非内部使用
		noWait:     false, // 默认阻塞等待确认
		args:       nil,   // 默认无额外参数
	}
}

// WithExchangeDeclareDurable 设置交换机持久化选项
func WithExchangeDeclareDurable(enable bool) ExchangeDeclareOption {
	return func(o *exchangeDeclareOptions) {
		o.durable = enable
	}
}

// WithExchangeDeclareAutoDelete 设置交换机自动删除选项
func WithExchangeDeclareAutoDelete(enable bool) ExchangeDeclareOption {
	return func(o *exchangeDeclareOptions) {
		o.autoDelete = enable
	}
}

// WithExchangeDeclareInternal set exchange declare internal option.
func WithExchangeDeclareInternal(enable bool) ExchangeDeclareOption {
	return func(o *exchangeDeclareOptions) {
		o.internal = enable
	}
}

// WithExchangeDeclareNoWait set exchange declare no wait option.
func WithExchangeDeclareNoWait(enable bool) ExchangeDeclareOption {
	return func(o *exchangeDeclareOptions) {
		o.noWait = enable
	}
}

// WithExchangeDeclareArgs set exchange declare args option.
func WithExchangeDeclareArgs(args map[string]interface{}) ExchangeDeclareOption {
	return func(o *exchangeDeclareOptions) {
		o.args = args
	}
}

// -------------------------------------------------------------------------------------------

// QueueDeclareOption 队列声明选项函数类型
type QueueDeclareOption func(*queueDeclareOptions)

type queueDeclareOptions struct {
	durable    bool       // durable 队列是否持久化，即使服务器重启也保留
	autoDelete bool       // autoDelete 是否自动删除，当最后一个消费者断开连接时是否自动删除
	exclusive  bool       // exclusive 是否排他，只有创建它的连接才能访问
	noWait     bool       // noWait 是否非阻塞处理，true表示不等待服务器确认
	args       amqp.Table // args 队列的其他属性参数
}

func (o *queueDeclareOptions) apply(opts ...QueueDeclareOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultQueueDeclareOptions 默认队列声明设置
func defaultQueueDeclareOptions() *queueDeclareOptions {
	return &queueDeclareOptions{
		durable:    true,  // 默认持久化
		autoDelete: false, // 默认不自动删除
		exclusive:  false, // 默认非排他
		noWait:     false, // 默认阻塞等待确认
		args:       nil,   // 默认无额外参数
	}
}

// WithQueueDeclareDurable set queue declare auto delete option.
func WithQueueDeclareDurable(enable bool) QueueDeclareOption {
	return func(o *queueDeclareOptions) {
		o.durable = enable
	}
}

// WithQueueDeclareAutoDelete set queue declare auto delete option.
func WithQueueDeclareAutoDelete(enable bool) QueueDeclareOption {
	return func(o *queueDeclareOptions) {
		o.autoDelete = enable
	}
}

// WithQueueDeclareExclusive set queue declare exclusive option.
func WithQueueDeclareExclusive(enable bool) QueueDeclareOption {
	return func(o *queueDeclareOptions) {
		o.exclusive = enable
	}
}

// WithQueueDeclareNoWait set queue declare no wait option.
func WithQueueDeclareNoWait(enable bool) QueueDeclareOption {
	return func(o *queueDeclareOptions) {
		o.noWait = enable
	}
}

// WithQueueDeclareArgs set queue declare args option.
func WithQueueDeclareArgs(args map[string]interface{}) QueueDeclareOption {
	return func(o *queueDeclareOptions) {
		o.args = args
	}
}

// -------------------------------------------------------------------------------------------

// QueueBindOption 队列绑定选项函数类型
type QueueBindOption func(*queueBindOptions)

type queueBindOptions struct {
	noWait bool       // noWait 是否非阻塞处理，true表示不等待服务器确认
	args   amqp.Table // args 绑定的其他属性参数，对于headers类型的交换机此参数无效
}

func (o *queueBindOptions) apply(opts ...QueueBindOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultQueueBindOptions 默认队列绑定设置
func defaultQueueBindOptions() *queueBindOptions {
	return &queueBindOptions{
		noWait: false, // 默认阻塞等待确认
		args:   nil,   // 默认无额外参数
	}
}

// WithQueueBindNoWait set queue bind no wait option.
func WithQueueBindNoWait(enable bool) QueueBindOption {
	return func(o *queueBindOptions) {
		o.noWait = enable
	}
}

// WithQueueBindArgs set queue bind args option.
func WithQueueBindArgs(args map[string]interface{}) QueueBindOption {
	return func(o *queueBindOptions) {
		o.args = args
	}
}

// -------------------------------------------------------------------------------------------

// NormalLetterOption 普通消息配置选项函数类型
type NormalLetterOption func(*NormalLetterOptions)

// NormalLetterOptions 普通消息配置选项结构
// 包含单一消息流转路径：消息→normalQueue，不涉及死信
type NormalLetterOptions struct {
	exchangeName     string // exchangeName 普通交换机名称
	normalQueueName  string // normalQueueName 普通队列名称
	normalRoutingKey string // normalRoutingKey 普通路由键

	exchangeDeclare    *exchangeDeclareOptions // exchangeDeclare 交换机声明选项
	normalQueueDeclare *queueDeclareOptions    // normalQueueDeclare 普通队列声明选项
	normalQueueBind    *queueBindOptions       // normalQueueBind 普通队列绑定选项
}

func (o *NormalLetterOptions) apply(opts ...NormalLetterOption) {
	for _, opt := range opts {
		opt(o)
	}
}

func defaultNormalLetterOptions() *NormalLetterOptions {
	return &NormalLetterOptions{
		exchangeName:       defaultExchangeName,             // 默认交换机名称
		exchangeDeclare:    defaultExchangeDeclareOptions(), // 默认交换机声明选项
		normalQueueName:    "normalQueue",                   // 默认普通队列名称
		normalRoutingKey:   "normalRouting",                 // 默认普通路由键
		normalQueueDeclare: defaultQueueDeclareOptions(),    // 默认普通队列声明选项
		normalQueueBind:    defaultQueueBindOptions(),       // 默认普通队列绑定选项
	}
}

// WithNormalLetterExchangeDeclareOptions set dead letter exchange declare option.
func WithNormalLetterExchangeDeclareOptions(opts ...ExchangeDeclareOption) NormalLetterOption {
	return func(o *NormalLetterOptions) {
		o.exchangeDeclare.apply(opts...)
	}
}

// WithNormalLetterNormalQueueDeclareOptions set dead letter queue declare option.
func WithNormalLetterNormalQueueDeclareOptions(opts ...QueueDeclareOption) NormalLetterOption {
	return func(o *NormalLetterOptions) {
		o.normalQueueDeclare.apply(opts...)
	}
}

// WithNormalLetterNormalQueueBindOptions set dead letter queue declare option.
func WithNormalLetterNormalQueueBindOptions(opts ...QueueBindOption) NormalLetterOption {
	return func(o *NormalLetterOptions) {
		o.normalQueueBind.apply(opts...)
	}
}

// WithNormalLetter set dead letter exchange, queue, routing key.
func WithNormalLetter(exchangeName string, normalQueueName string, normalRoutingKey string) NormalLetterOption {
	return func(o *NormalLetterOptions) {
		o.exchangeName = exchangeName
		o.normalQueueName = normalQueueName
		o.normalRoutingKey = normalRoutingKey
	}
}

// -------------------------------------------------------------------------------------------

// CustomerDeadLetterOption 自定义死信队列选项函数类型
type CustomerDeadLetterOption func(*CustomerDeadLetterOptions)

// CustomerDeadLetterOptions 自定义死信配置选项结构
// 在标准死信配置的基础上增加错误路由/队列支持，将重试消息与正常消息隔离到不同队列。
// 设计优势：重试消息不会影响正常队列的处理能力，避免大量重试拖垮正常消费吞吐。
// 三条消息流转路径：正常→normalQueue, 消费失败→deadQueue(死信TTL), TTL超时→errQueue(重试)
type CustomerDeadLetterOptions struct {
	// 嵌入标准死信配置，复用 deadRouting/deadQueue/normalQueue 等字段
	DeadLetterOptions

	errRoutingKey   string               // errRoutingKey 死信TTL超时后的重试路由键
	errQueueName    string               // errQueueName  死信TTL超时后的重试队列名称
	errQueueDeclare *queueDeclareOptions // errQueueDeclare 重试队列声明选项
	errQueueBind    *queueBindOptions    // errQueueBind    重试队列绑定选项
}

func (o *CustomerDeadLetterOptions) apply(opts ...CustomerDeadLetterOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultCustomerDeadLetterOptions 默认自定义死信配置
func defaultCustomerDeadLetterOptions() *CustomerDeadLetterOptions {
	base := defaultDeadLetterOptions()
	return &CustomerDeadLetterOptions{
		DeadLetterOptions: *base,
		errRoutingKey:     "errRouting",
		errQueueName:      "errQueue",
		errQueueDeclare:   defaultQueueDeclareOptions(),
		errQueueBind:      defaultQueueBindOptions(),
	}
}

// WithCustomerDeadLetterExchangeDeclareOptions set dead letter exchange declare option.
func WithCustomerDeadLetterExchangeDeclareOptions(opts ...ExchangeDeclareOption) CustomerDeadLetterOption {
	return func(o *CustomerDeadLetterOptions) {
		o.exchangeDeclare.apply(opts...)
	}
}

// WithCustomerDeadLetterDeadQueueDeclareOptions set dead letter queue declare option.
func WithCustomerDeadLetterDeadQueueDeclareOptions(opts ...QueueDeclareOption) CustomerDeadLetterOption {
	return func(o *CustomerDeadLetterOptions) {
		o.deadQueueDeclare.apply(opts...)
	}
}

// WithCustomerDeadLetterDeadQueueBindOptions set dead letter queue declare option.
func WithCustomerDeadLetterDeadQueueBindOptions(opts ...QueueBindOption) CustomerDeadLetterOption {
	return func(o *CustomerDeadLetterOptions) {
		o.deadQueueBind.apply(opts...)
	}
}

// WithCustomerDeadLetterErrQueueDeclareOptions set retry queue declare option.
func WithCustomerDeadLetterErrQueueDeclareOptions(opts ...QueueDeclareOption) CustomerDeadLetterOption {
	return func(o *CustomerDeadLetterOptions) {
		o.errQueueDeclare.apply(opts...)
	}
}

// WithCustomerDeadLetterErrQueueBindOptions set retry queue bind option.
func WithCustomerDeadLetterErrQueueBindOptions(opts ...QueueBindOption) CustomerDeadLetterOption {
	return func(o *CustomerDeadLetterOptions) {
		o.errQueueBind.apply(opts...)
	}
}

// WithCustomerDeadLetterNormalQueueDeclareOptions set dead letter queue declare option.
func WithCustomerDeadLetterNormalQueueDeclareOptions(opts ...QueueDeclareOption) CustomerDeadLetterOption {
	return func(o *CustomerDeadLetterOptions) {
		o.normalQueueDeclare.apply(opts...)
	}
}

// WithCustomerDeadLetterNormalQueueBindOptions set dead letter queue declare option.
func WithCustomerDeadLetterNormalQueueBindOptions(opts ...QueueBindOption) CustomerDeadLetterOption {
	return func(o *CustomerDeadLetterOptions) {
		o.normalQueueBind.apply(opts...)
	}
}

// WithCustomerDeadLetter set dead letter exchange, queue, routing key.
func WithCustomerDeadLetter(
	exchangeName string,
	deadQueueName string,
	deadRoutingKey string,
	errQueueName string,
	errRoutingKey string,
	normalQueueName string,
	normalRoutingKey string,
) CustomerDeadLetterOption {
	return func(o *CustomerDeadLetterOptions) {
		o.exchangeName = exchangeName

		o.deadQueueName = deadQueueName
		o.deadRoutingKey = deadRoutingKey
		o.errRoutingKey = errRoutingKey
		o.errQueueName = errQueueName
		o.normalQueueName = normalQueueName
		o.normalRoutingKey = normalRoutingKey
	}
}

// -------------------------------------------------------------------------------------------

// DeadLetterOption 死信队列选项函数类型
type DeadLetterOption func(*DeadLetterOptions)

// DeadLetterOptions 死信队列配置选项结构
// 包含正常+死信两条消息路径，形成消费失败重试循环：
//   - 正常投递 → normalQueue（消费失败→死信队列）
//   - TTL超时 → deadQueue → TTL超时后重新投递到 normalQueue（循环重试）
type DeadLetterOptions struct {
	exchangeName string // exchangeName 死信交换机名称

	deadRoutingKey   string // deadRoutingKey 死信路由键
	deadQueueName    string // deadQueueName 死信队列名称
	normalQueueName  string // normalQueueName 普通队列名称
	normalRoutingKey string // normalRoutingKey 普通路由键

	exchangeDeclare    *exchangeDeclareOptions // exchangeDeclare 交换机声明选项
	deadQueueDeclare   *queueDeclareOptions    // deadQueueDeclare 死信队列声明选项
	deadQueueBind      *queueBindOptions       // deadQueueBind 死信队列绑定选项
	normalQueueDeclare *queueDeclareOptions    // normalQueueDeclare 普通队列声明选项
	normalQueueBind    *queueBindOptions       // normalQueueBind 普通队列绑定选项
}

func (o *DeadLetterOptions) apply(opts ...DeadLetterOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultDeadLetterOptions 默认死信队列配置
func defaultDeadLetterOptions() *DeadLetterOptions {
	return &DeadLetterOptions{
		exchangeName:       defaultExchangeName,
		exchangeDeclare:    defaultExchangeDeclareOptions(),
		deadRoutingKey:     "deadRouting",
		deadQueueName:      "deadQueue",
		normalQueueName:    "normalQueue",
		normalRoutingKey:   "normalRouting",
		deadQueueDeclare:   defaultQueueDeclareOptions(),
		deadQueueBind:      defaultQueueBindOptions(),
		normalQueueDeclare: defaultQueueDeclareOptions(),
		normalQueueBind:    defaultQueueBindOptions(),
	}
}

// WithDeadLetterExchangeDeclareOptions 设置死信交换机声明选项
func WithDeadLetterExchangeDeclareOptions(opts ...ExchangeDeclareOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.exchangeDeclare.apply(opts...)
	}
}

// WithDeadLetterDeadQueueDeclareOptions 设置死信队列声明选项
func WithDeadLetterDeadQueueDeclareOptions(opts ...QueueDeclareOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.deadQueueDeclare.apply(opts...)
	}
}

// WithDeadLetterDeadQueueBindOptions 设置死信队列绑定选项
func WithDeadLetterDeadQueueBindOptions(opts ...QueueBindOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.deadQueueBind.apply(opts...)
	}
}

// WithDeadLetterNormalQueueDeclareOptions 设置普通队列声明选项
func WithDeadLetterNormalQueueDeclareOptions(opts ...QueueDeclareOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.normalQueueDeclare.apply(opts...)
	}
}

// WithDeadLetterNormalQueueBindOptions 设置普通队列绑定选项
func WithDeadLetterNormalQueueBindOptions(opts ...QueueBindOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.normalQueueBind.apply(opts...)
	}
}

// WithDeadLetter 设置死信队列的交换机、队列、路由键
func WithDeadLetter(exchangeName string, deadQueueName string, deadRoutingKey string, normalQueueName string, normalRoutingKey string) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.exchangeName = exchangeName
		o.deadQueueName = deadQueueName
		o.deadRoutingKey = deadRoutingKey
		o.normalQueueName = normalQueueName
		o.normalRoutingKey = normalRoutingKey
	}
}

// -------------------------------------------------------------------------------------------

// ConsumeOption 消费选项类型
type ConsumeOption func(*consumeOptions)

// consumeOptions 消费配置选项结构体
type consumeOptions struct {
	consumer  string     // consumer 消费者标识，用于区分多个消费者
	exclusive bool       // exclusive 是否独占，只有创建它的程序才能访问
	noLocal   bool       // noLocal 如果设置为true，同一个Connection中的生产者发送的消息不能传递给该Connection中的消费者
	noWait    bool       // noWait 是否阻塞处理
	args      amqp.Table // args 额外属性
}

// apply 应用消费选项
func (o *consumeOptions) apply(opts ...ConsumeOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultConsumeOptions 默认消费设置
func defaultConsumeOptions() *consumeOptions {
	return &consumeOptions{
		consumer:  "",    // 默认消费者标识为空
		exclusive: false, // 默认非独占
		noLocal:   false, // 默认允许本地消费
		noWait:    false, // 默认阻塞处理
		args:      nil,   // 默认无额外参数
	}
}

// WithConsumeConsumer 设置消费消费者选项
func WithConsumeConsumer(consumer string) ConsumeOption {
	return func(o *consumeOptions) {
		o.consumer = consumer
	}
}

// WithConsumeExclusive 设置消费独占选项
func WithConsumeExclusive(enable bool) ConsumeOption {
	return func(o *consumeOptions) {
		o.exclusive = enable
	}
}

// WithConsumeNoLocal 设置消费noLocal选项
func WithConsumeNoLocal(enable bool) ConsumeOption {
	return func(o *consumeOptions) {
		o.noLocal = enable
	}
}

// WithConsumeNoWait 设置消费不等待选项
func WithConsumeNoWait(enable bool) ConsumeOption {
	return func(o *consumeOptions) {
		o.noWait = enable
	}
}

// WithConsumeArgs 设置消费参数选项
func WithConsumeArgs(args map[string]interface{}) ConsumeOption {
	return func(o *consumeOptions) {
		o.args = args
	}
}

// -------------------------------------------------------------------------------------------

// QosOption QoS选项类型
// 用于配置RabbitMQ消费者的QoS（服务质量）参数
type QosOption func(*qosOptions)

// qosOptions QoS配置选项结构体
// 包含所有与QoS相关的配置参数
type qosOptions struct {
	enable        bool // enable 是否启用QoS功能
	prefetchCount int  // prefetchCount 预取消息数量，0表示无限制
	prefetchSize  int  // prefetchSize 预取消息大小，0表示无限制
	global        bool // global 是否全局生效（对整个通道生效，而不仅仅是当前消费者）
}

// apply 应用QoS选项
func (o *qosOptions) apply(opts ...QosOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultQosOptions 默认QoS设置
// 返回包含默认QoS配置的选项结构体
func defaultQosOptions() *qosOptions {
	return &qosOptions{
		enable:        false, // 默认不启用QoS
		prefetchCount: 1,     //指定消费者可以同时处理的消息数量上限。例如，如果设置为 1，则消费者在处理完前 1 条消息之前不会接收新的消息。 prefetchCount 为 0，表示不限制消息数量
		prefetchSize:  0,     // 默认预取消息大小无限制
		global:        false, // 默认仅对当前消费者生效
	}
}

// WithQosEnable 设置启用QoS功能选项
// 用于开启消费者的QoS（服务质量）控制，配合其他QoS选项使用可以限制消费者预取消息的数量，
// 实现流量控制、负载均衡和资源管理，防止消费者被大量消息淹没
func WithQosEnable() QosOption {
	return func(o *qosOptions) {
		o.enable = true
	}
}

// WithQosPrefetchCount 设置QoS预取消息数量选项
// 控制消费者在任意时刻可以预取并处理的最大消息数量
// prefetchCount > 0 时启用，值为0表示无限制
// 通过限制预取消息数量可以实现消费者间的负载均衡
func WithQosPrefetchCount(count int) QosOption {
	return func(o *qosOptions) {
		o.prefetchCount = count
	}
}

// WithQosPrefetchSize 设置QoS预取消息大小选项
// 控制消费者在任意时刻可以预取消息的总大小（以字节为单位）
// prefetchSize > 0 时启用，值为0表示无限制
// 注意：该参数在RabbitMQ中很少使用，通常设置为0
func WithQosPrefetchSize(size int) QosOption {
	return func(o *qosOptions) {
		o.prefetchSize = size
	}
}

// setupCustomerDeadLetterDeclare 声明自定义死信队列的三条消息路径并绑定到交换机。
// 包含：死信队列（消费失败后转入，带TTL）、重试队列（死信TTL超时后重试）、普通队列（正常消费）。
// 设计优势：通过独立的重试队列隔离失败消息，保障正常队列始终专注于处理新消息，不受重试流量干扰。
//
// 参数:
//   - channel: AMQP 通道
//   - exchangeName: 交换机名称
//   - exchangeType: 交换机类型
//   - opts: 自定义死信队列配置选项
func setupCustomerDeadLetterDeclare(channel *amqp.Channel, exchangeName, exchangeType string, opts *CustomerDeadLetterOptions) error {
	// 声明主交换机
	if err := channel.ExchangeDeclare(
		exchangeName,
		exchangeType,
		opts.exchangeDeclare.durable,
		opts.exchangeDeclare.autoDelete,
		opts.exchangeDeclare.internal,
		opts.exchangeDeclare.noWait,
		opts.exchangeDeclare.args,
	); err != nil {
		return err
	}

	// 声明死信队列（正常/重试队列消费失败后转入，TTL超时后转至重试队列）并绑定到交换机
	if opts.deadQueueDeclare.args == nil {
		opts.deadQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchangeName,
			"x-dead-letter-routing-key": opts.errRoutingKey,
			"x-message-ttl":             int32(600000),
		}
	}
	dlq, err := channel.QueueDeclare(
		opts.deadQueueName,
		opts.deadQueueDeclare.durable,
		opts.deadQueueDeclare.autoDelete,
		opts.deadQueueDeclare.exclusive,
		opts.deadQueueDeclare.noWait,
		opts.deadQueueDeclare.args,
	)
	if err != nil {
		return err
	}
	if err = channel.QueueBind(
		dlq.Name,
		opts.deadRoutingKey,
		exchangeName,
		opts.deadQueueBind.noWait,
		opts.deadQueueBind.args,
	); err != nil {
		return err
	}

	// 声明重试队列（死信TTL超时后重新投递到此处，再次消费失败则回到死信队列）并绑定到交换机
	if opts.errQueueDeclare.args == nil {
		opts.errQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchangeName,
			"x-dead-letter-routing-key": opts.deadRoutingKey,
		}
	}
	elq, err := channel.QueueDeclare(
		opts.errQueueName,
		opts.errQueueDeclare.durable,
		opts.errQueueDeclare.autoDelete,
		opts.errQueueDeclare.exclusive,
		opts.errQueueDeclare.noWait,
		opts.errQueueDeclare.args,
	)
	if err != nil {
		return err
	}
	if err = channel.QueueBind(
		elq.Name,
		opts.errRoutingKey,
		exchangeName,
		opts.errQueueBind.noWait,
		opts.errQueueBind.args,
	); err != nil {
		return err
	}

	// 声明普通队列（正常消费消息入队）并绑定到交换机
	if opts.normalQueueDeclare.args == nil {
		opts.normalQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchangeName,
			"x-dead-letter-routing-key": opts.deadRoutingKey,
		}
	}
	nlq, err := channel.QueueDeclare(
		opts.normalQueueName,
		opts.normalQueueDeclare.durable,
		opts.normalQueueDeclare.autoDelete,
		opts.normalQueueDeclare.exclusive,
		opts.normalQueueDeclare.noWait,
		opts.normalQueueDeclare.args,
	)
	if err != nil {
		return err
	}
	return channel.QueueBind(
		nlq.Name,
		opts.normalRoutingKey,
		exchangeName,
		opts.normalQueueBind.noWait,
		opts.normalQueueBind.args,
	)
}

// setupStandardDeadLetterDeclare 声明标准死信队列的两条消息路径并绑定到交换机。
// 包含：死信队列（消费失败后转入，TTL超时后重投递到普通队列）、普通队列（正常消费/死信重试）。
// 形成 正常↔死信 两队列循环重试架构。
//
// 参数:
//   - channel: AMQP 通道
//   - exchangeName: 交换机名称
//   - exchangeType: 交换机类型
//   - opts: 标准死信队列配置选项
func setupStandardDeadLetterDeclare(channel *amqp.Channel, exchangeName, exchangeType string, opts *DeadLetterOptions) error {
	// 声明主交换机
	if err := channel.ExchangeDeclare(
		exchangeName,
		exchangeType,
		opts.exchangeDeclare.durable,
		opts.exchangeDeclare.autoDelete,
		opts.exchangeDeclare.internal,
		opts.exchangeDeclare.noWait,
		opts.exchangeDeclare.args,
	); err != nil {
		return err
	}

	// 声明死信队列（正常队列消费失败后转入，TTL超时后重新投递到普通队列）并绑定到交换机
	if opts.deadQueueDeclare.args == nil {
		opts.deadQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchangeName,
			"x-dead-letter-routing-key": opts.normalRoutingKey,
			"x-message-ttl":             int32(600000),
		}
	}
	dlq, err := channel.QueueDeclare(
		opts.deadQueueName,
		opts.deadQueueDeclare.durable,
		opts.deadQueueDeclare.autoDelete,
		opts.deadQueueDeclare.exclusive,
		opts.deadQueueDeclare.noWait,
		opts.deadQueueDeclare.args,
	)
	if err != nil {
		return err
	}
	if err = channel.QueueBind(
		dlq.Name,
		opts.deadRoutingKey,
		exchangeName,
		opts.deadQueueBind.noWait,
		opts.deadQueueBind.args,
	); err != nil {
		return err
	}

	// 声明普通队列（正常消费/死信TTL超时后重试消息入队）并绑定到交换机
	if opts.normalQueueDeclare.args == nil {
		opts.normalQueueDeclare.args = amqp.Table{
			"x-dead-letter-exchange":    exchangeName,
			"x-dead-letter-routing-key": opts.deadRoutingKey,
		}
	}
	nlq, err := channel.QueueDeclare(
		opts.normalQueueName,
		opts.normalQueueDeclare.durable,
		opts.normalQueueDeclare.autoDelete,
		opts.normalQueueDeclare.exclusive,
		opts.normalQueueDeclare.noWait,
		opts.normalQueueDeclare.args,
	)
	if err != nil {
		return err
	}
	return channel.QueueBind(
		nlq.Name,
		opts.normalRoutingKey,
		exchangeName,
		opts.normalQueueBind.noWait,
		opts.normalQueueBind.args,
	)
}

// setupNormalLetterDeclare 声明正常队列的单条消息路径并绑定到交换机。
// 包含：交换机声明 + 队列声明 + 绑定。
//
// 参数:
//   - channel: AMQP 通道
//   - exchangeName: 交换机名称
//   - exchangeType: 交换机类型
//   - opts: 正常队列配置选项
func setupNormalLetterDeclare(channel *amqp.Channel, exchangeName, exchangeType string, opts *NormalLetterOptions) error {
	// 声明主交换机
	if err := channel.ExchangeDeclare(
		exchangeName,
		exchangeType,
		opts.exchangeDeclare.durable,
		opts.exchangeDeclare.autoDelete,
		opts.exchangeDeclare.internal,
		opts.exchangeDeclare.noWait,
		opts.exchangeDeclare.args,
	); err != nil {
		return err
	}

	// 声明队列并绑定到交换机
	nlq, err := channel.QueueDeclare(
		opts.normalQueueName,
		opts.normalQueueDeclare.durable,
		opts.normalQueueDeclare.autoDelete,
		opts.normalQueueDeclare.exclusive,
		opts.normalQueueDeclare.noWait,
		opts.normalQueueDeclare.args,
	)
	if err != nil {
		return err
	}
	return channel.QueueBind(
		nlq.Name,
		opts.normalRoutingKey,
		exchangeName,
		opts.normalQueueBind.noWait,
		opts.normalQueueBind.args,
	)
}

// WithQosPrefetchGlobal 设置QoS全局生效选项
// 控制QoS设置是否应用于整个通道（true）还是仅应用于当前消费者（false）
// 当设置为true时，QoS设置将应用于该通道上的所有消费者
func WithQosPrefetchGlobal(enable bool) QosOption {
	return func(o *qosOptions) {
		o.global = enable
	}
}
