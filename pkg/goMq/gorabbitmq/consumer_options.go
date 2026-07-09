package gorabbitmq

// ConsumerOption 消费者选项配置函数类型
type ConsumerOption func(*consumerOptions)

// consumerOptions 消费者配置选项
type consumerOptions struct {
	customerDeadLetter *CustomerDeadLetterOptions // 自定义死信队列选项，包含死信交换机、死信队列、异常队列的声明和绑定配置
	normalLetter       *NormalLetterOptions       // 正常队列选项，包含普通交换机和队列的声明与绑定配置
	deadLetter         *DeadLetterOptions         // 标准死信队列选项，包含死信交换机、死信队列、正常队列的声明与绑定配置
	qos                *qosOptions                // 消费者 QoS 选项，包含预取数量和大小等流控配置
	consume            *consumeOptions            // 消费选项，包含消费者名称、排他性、noLocal 等消费行为配置

	msgDurable bool   // 消息持久化标志，true 表示消息写入磁盘，false 表示仅内存存储
	isAutoAck  bool   // 自动确认标志，true 表示消费后自动 Ack，false 需手动 Ack/Nack
	name       string // 消费者名称，用于 OpenTelemetry trace span 标识
}

// apply 应用消费者选项
func (o *consumerOptions) apply(opts ...ConsumerOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultConsumerOptions 默认消费者设置
func defaultConsumerOptions() *consumerOptions {
	return &consumerOptions{
		customerDeadLetter: defaultCustomerDeadLetterOptions(),
		normalLetter:       defaultNormalLetterOptions(),
		deadLetter:         defaultDeadLetterOptions(),
		qos:                defaultQosOptions(),
		consume:            defaultConsumeOptions(),
		msgDurable:         true,
		isAutoAck:          true,
		name:               "",
	}
}

// WithConsumerCustomerDeadLetterOptions set dead letter options.
func WithConsumerCustomerDeadLetterOptions(opts ...CustomerDeadLetterOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.customerDeadLetter.apply(opts...)
	}
}

// WithConsumerNormalLetterOptions set dead letter options.
func WithConsumerNormalLetterOptions(opts ...NormalLetterOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.normalLetter.apply(opts...)
	}
}

// WithConsumerDeadLetterOptions set dead letter options.
func WithConsumerDeadLetterOptions(opts ...DeadLetterOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.deadLetter.apply(opts...)
	}
}

// WithConsumerQosOptions 设置消费QoS选项
func WithConsumerQosOptions(opts ...QosOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.qos.apply(opts...)
	}
}

// WithConsumerConsumeOptions 设置消费者消费选项
func WithConsumerConsumeOptions(opts ...ConsumeOption) ConsumerOption {
	return func(o *consumerOptions) {
		o.consume.apply(opts...)
	}
}

// WithConsumerAutoAck 设置消费者自动确认选项
func WithConsumerAutoAck(enable bool) ConsumerOption {
	return func(o *consumerOptions) {
		o.isAutoAck = enable
	}
}

// WithConsumerMsgDurable 设置消费者消息持久化选项
func WithConsumerMsgDurable(enable bool) ConsumerOption {
	return func(o *consumerOptions) {
		o.msgDurable = enable
	}
}

// WithConsumerName 设置消费者名称，用于 trace span
func WithConsumerName(name string) ConsumerOption {
	return func(o *consumerOptions) {
		o.name = name
	}
}
