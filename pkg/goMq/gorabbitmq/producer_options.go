package gorabbitmq

// producerOptions 生产者配置选项，NewProducer 收集后嵌入到 Producer 结构体。
type producerOptions struct {
	customerDeadLetter *CustomerDeadLetterOptions // 自定义死信队列（含正常/死信/异常三条消息路径）
	normalLetter       *NormalLetterOptions       // 普通队列（声明队列并绑定到交换机）
	deadLetter         *DeadLetterOptions         // 标准死信队列（含死信+普通两条路径）
	msgDurable         bool                       // true=消息持久化到磁盘（amqp.Persistent），false=仅内存（amqp.Transient）
	mandatory          bool                       // true=消息不可路由时回退给发送者，false=直接丢弃不可路由的消息
	isDelay            bool                       // true=启用延迟消息（需配合 x-delayed-message 类型交换机使用）
}

// ProducerOption 生产者配置选项函数类型
type ProducerOption func(*producerOptions)

// apply 应用生产者配置选项
func (o *producerOptions) apply(opts ...ProducerOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultProducerOptions 默认生产者配置选项
func defaultProducerOptions() *producerOptions {
	return &producerOptions{
		customerDeadLetter: defaultCustomerDeadLetterOptions(),
		normalLetter:       defaultNormalLetterOptions(),
		deadLetter:         defaultDeadLetterOptions(),
		//用于控制消息是否持久化存储。当设置为 true 时，表示消息需要被持久化，以确保在 RabbitMQ 服务器重启后消息不会丢失。
		msgDurable: true,
		//mandatory 设置为 true 时，如果消息无法根据 exchange 类型和 routing key 规则路由到任何队列，消息会被返回给发送者
		//mandatory 设置为 false 时，无法路由的消息会被直接丢弃
		mandatory: true,
		isDelay:   false,
	}
}

// WithProducerCustomerDeadLetterOptions set dead letter options.
func WithProducerCustomerDeadLetterOptions(opts ...CustomerDeadLetterOption) ProducerOption {
	return func(o *producerOptions) {
		o.customerDeadLetter.apply(opts...)
	}
}

// WithProducerDeadLetterOptions set dead letter options.
func WithProducerDeadLetterOptions(opts ...DeadLetterOption) ProducerOption {
	return func(o *producerOptions) {
		o.deadLetter.apply(opts...)
	}
}

// WithProducerNormalLetterOptions set dead letter options.
func WithProducerNormalLetterOptions(opts ...NormalLetterOption) ProducerOption {
	return func(o *producerOptions) {
		o.normalLetter.apply(opts...)
	}
}

// WithProducerMsgDurable 消息是否持久化 set producer persistent option.
func WithProducerMsgDurable(enable bool) ProducerOption {
	return func(o *producerOptions) {
		o.msgDurable = enable
	}
}

// WithProducerMandatory  消息不可路由时是否返回给发送者 set producer mandatory option.
func WithProducerMandatory(enable bool) ProducerOption {
	return func(o *producerOptions) {
		o.mandatory = enable
	}
}

// WithProducerIsDelay 设置生产者是否启用延迟消息
func WithProducerIsDelay(enable bool) ProducerOption {
	return func(o *producerOptions) {
		o.isDelay = enable
	}
}
