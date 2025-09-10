package gorabbitmq

import amqp "github.com/rabbitmq/amqp091-go"

// ErrClosed closed
var ErrClosed = amqp.ErrClosed

const (
	exchangeTypeDirect         = "direct"
	exchangeTypeTopic          = "topic"
	exchangeTypeFanout         = "fanout"
	exchangeTypeHeaders        = "headers"
	exchangeTypeDelayedMessage = "x-delayed-message"

	// HeadersTypeAll all
	HeadersTypeAll HeadersType = "all"
	// HeadersTypeAny any
	HeadersTypeAny HeadersType = "any"
)

// HeadersType headers type
type HeadersType = string

// Exchange rabbitmq minimum management unit
type Exchange struct {
	name        string                 // exchange name
	eType       string                 // exchange type: direct, topic, fanout, headers, x-delayed-message
	routingKey  string                 // route key
	headersKeys map[string]interface{} // this field is required if eType=headers.
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
	durable    bool
	autoDelete bool       // delete automatically
	internal   bool       // public or not, false means public
	noWait     bool       // block processing
	args       amqp.Table // additional properties
}

func (o *exchangeDeclareOptions) apply(opts ...ExchangeDeclareOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// default exchange declare settings
func defaultExchangeDeclareOptions() *exchangeDeclareOptions {
	return &exchangeDeclareOptions{
		durable:    true,
		autoDelete: false,
		internal:   false,
		noWait:     false,
		args:       nil,
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
	durable    bool
	autoDelete bool       // delete automatically
	exclusive  bool       // exclusive (only available to the program that created it)
	noWait     bool       // block processing
	args       amqp.Table // additional properties
}

func (o *queueDeclareOptions) apply(opts ...QueueDeclareOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// default queue declare settings
func defaultQueueDeclareOptions() *queueDeclareOptions {
	return &queueDeclareOptions{
		durable:    true,
		autoDelete: false,
		exclusive:  false,
		noWait:     false,
		args:       nil,
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
	noWait bool       // block processing
	args   amqp.Table // this parameter is invalid if the type is headers.
}

func (o *queueBindOptions) apply(opts ...QueueBindOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// default queue bind settings
func defaultQueueBindOptions() *queueBindOptions {
	return &queueBindOptions{
		noWait: false,
		args:   nil,
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
	exchangeName     string
	normalQueueName  string
	normalRoutingKey string

	exchangeDeclare    *exchangeDeclareOptions
	normalQueueDeclare *queueDeclareOptions
	normalQueueBind    *queueBindOptions
}

func (o *NormalLetterOptions) apply(opts ...NormalLetterOption) {
	for _, opt := range opts {
		opt(o)
	}
}

func defaultNormalLetterOptions() *NormalLetterOptions {
	return &NormalLetterOptions{
		exchangeName:       "sunshine",
		exchangeDeclare:    defaultExchangeDeclareOptions(),
		normalQueueName:    "normalQueue",
		normalRoutingKey:   "normalRouting",
		normalQueueDeclare: defaultQueueDeclareOptions(),
		normalQueueBind:    defaultQueueBindOptions(),
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
	exchangeName string

	deadRoutingKey   string
	deadQueueName    string
	errRoutingKey    string
	errQueueName     string
	normalQueueName  string
	normalRoutingKey string

	exchangeDeclare    *exchangeDeclareOptions
	deadQueueDeclare   *queueDeclareOptions
	deadQueueBind      *queueBindOptions
	errQueueDeclare    *queueDeclareOptions
	errQueueBind       *queueBindOptions
	normalQueueDeclare *queueDeclareOptions
	normalQueueBind    *queueBindOptions
}

func (o *CustomerDeadLetterOptions) apply(opts ...CustomerDeadLetterOption) {
	for _, opt := range opts {
		opt(o)
	}
}

func defaultCustomerDeadLetterOptions() *CustomerDeadLetterOptions {
	return &CustomerDeadLetterOptions{
		exchangeName:       "sunshine",
		exchangeDeclare:    defaultExchangeDeclareOptions(),
		deadRoutingKey:     "deadRouting",
		deadQueueName:      "deadQueue",
		errRoutingKey:      "errRouting",
		errQueueName:       "errQueue",
		normalQueueName:    "normalQueue",
		normalRoutingKey:   "normalRouting",
		deadQueueDeclare:   defaultQueueDeclareOptions(),
		deadQueueBind:      defaultQueueBindOptions(),
		errQueueDeclare:    defaultQueueDeclareOptions(),
		errQueueBind:       defaultQueueBindOptions(),
		normalQueueDeclare: defaultQueueDeclareOptions(),
		normalQueueBind:    defaultQueueBindOptions(),
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

// ConsumeOption 消费选项类型
type ConsumeOption func(*consumeOptions)

// consumeOptions 消费配置选项结构体
type consumeOptions struct {
	consumer  string     // 用于区分多个消费者
	exclusive bool       // 是否独占，只有创建它的程序才能访问
	noLocal   bool       // 如果设置为true，同一个Connection中的生产者发送的消息不能传递给该Connection中的消费者
	noWait    bool       // 是否阻塞处理
	args      amqp.Table // 额外属性
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
		consumer:  "",
		exclusive: false,
		noLocal:   false,
		noWait:    false,
		args:      nil,
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
	enable        bool // 是否启用QoS功能
	prefetchCount int  // 预取消息数量，0表示无限制
	prefetchSize  int  // 预取消息大小，0表示无限制
	global        bool // 是否全局生效（对整个通道生效，而不仅仅是当前消费者）
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
		enable:        false,
		prefetchCount: 0,
		prefetchSize:  0,
		global:        false,
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
