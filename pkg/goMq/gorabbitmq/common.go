package gorabbitmq

import amqp "github.com/rabbitmq/amqp091-go"

// ErrClosed closed
var ErrClosed = amqp.ErrClosed

const (
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

// HeadersType headers type
type HeadersType = string // HeadersType 头交换机类型别名

// Exchange rabbitmq minimum management unit
type Exchange struct {
	name        string                 // name 交换机名称
	eType       string                 // eType 交换机类型: direct, topic, fanout, headers, x-delayed-message
	routingKey  string                 // routingKey 路由键
	headersKeys map[string]interface{} // headersKeys 头交换机的键值对配置
}

// Name exchange name
func (e *Exchange) Name() string {
	return e.name
}

// Type exchange type
func (e *Exchange) Type() string {
	return e.eType
}

// RoutingKey exchange routing key
func (e *Exchange) RoutingKey() string {
	return e.routingKey
}

// HeadersKeys exchange headers keys
func (e *Exchange) HeadersKeys() map[string]interface{} {
	return e.headersKeys
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

// ExchangeDeclareOption declare exchange option.
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

// default exchange declare settings
func defaultExchangeDeclareOptions() *exchangeDeclareOptions {
	return &exchangeDeclareOptions{
		durable:    true,  // 默认持久化
		autoDelete: false, // 默认不自动删除
		internal:   false, // 默认非内部使用
		noWait:     false, // 默认阻塞等待确认
		args:       nil,   // 默认无额外参数
	}
}

// WithExchangeDeclareDurable set exchange declare auto delete option.
func WithExchangeDeclareDurable(enable bool) ExchangeDeclareOption {
	return func(o *exchangeDeclareOptions) {
		o.durable = enable
	}
}

// WithExchangeDeclareAutoDelete set exchange declare auto delete option.
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

// QueueDeclareOption declare queue option.
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

// default queue declare settings
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

// QueueBindOption declare queue bind option.
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

// default queue bind settings
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

// NormalLetterOption declare dead letter option.
type NormalLetterOption func(*NormalLetterOptions)

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
		exchangeName:       "sunshine",                      // 默认交换机名称
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

// CustomerDeadLetterOption declare dead letter option.
type CustomerDeadLetterOption func(*CustomerDeadLetterOptions)

type CustomerDeadLetterOptions struct {
	exchangeName string // exchangeName 死信交换机名称

	deadRoutingKey   string // deadRoutingKey 死信路由键
	deadQueueName    string // deadQueueName 死信队列名称
	errRoutingKey    string // errRoutingKey 错误路由键
	errQueueName     string // errQueueName 错误队列名称
	normalQueueName  string // normalQueueName 普通队列名称
	normalRoutingKey string // normalRoutingKey 普通路由键

	exchangeDeclare    *exchangeDeclareOptions // exchangeDeclare 交换机声明选项
	deadQueueDeclare   *queueDeclareOptions    // deadQueueDeclare 死信队列声明选项
	deadQueueBind      *queueBindOptions       // deadQueueBind 死信队列绑定选项
	errQueueDeclare    *queueDeclareOptions    // errQueueDeclare 错误队列声明选项
	errQueueBind       *queueBindOptions       // errQueueBind 错误队列绑定选项
	normalQueueDeclare *queueDeclareOptions    // normalQueueDeclare 普通队列声明选项
	normalQueueBind    *queueBindOptions       // normalQueueBind 普通队列绑定选项
}

func (o *CustomerDeadLetterOptions) apply(opts ...CustomerDeadLetterOption) {
	for _, opt := range opts {
		opt(o)
	}
}

func defaultCustomerDeadLetterOptions() *CustomerDeadLetterOptions {
	return &CustomerDeadLetterOptions{
		exchangeName:       "sunshine",                      // 默认死信交换机名称
		exchangeDeclare:    defaultExchangeDeclareOptions(), // 默认交换机声明选项
		deadRoutingKey:     "deadRouting",                   // 默认死信路由键
		deadQueueName:      "deadQueue",                     // 默认死信队列名称
		errRoutingKey:      "errRouting",                    // 默认错误路由键
		errQueueName:       "errQueue",                      // 默认错误队列名称
		normalQueueName:    "normalQueue",                   // 默认普通队列名称
		normalRoutingKey:   "normalRouting",                 // 默认普通路由键
		deadQueueDeclare:   defaultQueueDeclareOptions(),    // 默认死信队列声明选项
		deadQueueBind:      defaultQueueBindOptions(),       // 默认死信队列绑定选项
		errQueueDeclare:    defaultQueueDeclareOptions(),    // 默认错误队列声明选项
		errQueueBind:       defaultQueueBindOptions(),       // 默认错误队列绑定选项
		normalQueueDeclare: defaultQueueDeclareOptions(),    // 默认普通队列声明选项
		normalQueueBind:    defaultQueueBindOptions(),       // 默认普通队列绑定选项
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

// WithCustomerDeadLetterErrQueueDeclareOptions set dead letter queue declare option.
func WithCustomerDeadLetterErrQueueDeclareOptions(opts ...QueueDeclareOption) CustomerDeadLetterOption {
	return func(o *CustomerDeadLetterOptions) {
		o.errQueueDeclare.apply(opts...)
	}
}

// WithCustomerDeadLetterErrQueueBindOptions set dead letter queue declare option.
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
func WithCustomerDeadLetter(exchangeName string, deadQueueName string, deadRoutingKey string, errQueueName string, errRoutingKey string, normalQueueName string, normalRoutingKey string) CustomerDeadLetterOption {
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

// DeadLetterOption declare dead letter option.
type DeadLetterOption func(*DeadLetterOptions)

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

func defaultDeadLetterOptions() *DeadLetterOptions {
	return &DeadLetterOptions{
		exchangeName:       "sunshine",                      // 默认死信交换机名称
		exchangeDeclare:    defaultExchangeDeclareOptions(), // 默认交换机声明选项
		deadRoutingKey:     "deadRouting",                   // 默认死信路由键
		deadQueueName:      "deadQueue",                     // 默认死信队列名称
		normalQueueName:    "normalQueue",                   // 默认普通队列名称
		normalRoutingKey:   "normalRouting",                 // 默认普通路由键
		deadQueueDeclare:   defaultQueueDeclareOptions(),    // 默认死信队列声明选项
		deadQueueBind:      defaultQueueBindOptions(),       // 默认死信队列绑定选项
		normalQueueDeclare: defaultQueueDeclareOptions(),    // 默认普通队列声明选项
		normalQueueBind:    defaultQueueBindOptions(),       // 默认普通队列绑定选项
	}
}

// WithDeadLetterExchangeDeclareOptions set dead letter exchange declare option.
func WithDeadLetterExchangeDeclareOptions(opts ...ExchangeDeclareOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.exchangeDeclare.apply(opts...)
	}
}

// WithDeadLetterDeadQueueDeclareOptions set dead letter queue declare option.
func WithDeadLetterDeadQueueDeclareOptions(opts ...QueueDeclareOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.deadQueueDeclare.apply(opts...)
	}
}

// WithDeadLetterDeadQueueBindOptions set dead letter queue declare option.
func WithDeadLetterDeadQueueBindOptions(opts ...QueueBindOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.deadQueueBind.apply(opts...)
	}
}

// WithDeadLetterNormalQueueDeclareOptions set dead letter queue declare option.
func WithDeadLetterNormalQueueDeclareOptions(opts ...QueueDeclareOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.normalQueueDeclare.apply(opts...)
	}
}

// WithDeadLetterNormalQueueBindOptions set dead letter queue declare option.
func WithDeadLetterNormalQueueBindOptions(opts ...QueueBindOption) DeadLetterOption {
	return func(o *DeadLetterOptions) {
		o.normalQueueBind.apply(opts...)
	}
}

// WithDeadLetter set dead letter exchange, queue, routing key.
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

// WithQosPrefetchGlobal 设置QoS全局生效选项
// 控制QoS设置是否应用于整个通道（true）还是仅应用于当前消费者（false）
// 当设置为true时，QoS设置将应用于该通道上的所有消费者
func WithQosPrefetchGlobal(enable bool) QosOption {
	return func(o *qosOptions) {
		o.global = enable
	}
}
