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
	return &Exchange{
		name:       exchangeName,
		eType:      exchangeTypeDirect,
		routingKey: routingKey,
	}
}

// NewTopicExchange create a topic exchange
func NewTopicExchange(exchangeName string, routingKey string) *Exchange {
	return &Exchange{
		name:       exchangeName,
		eType:      exchangeTypeTopic,
		routingKey: routingKey,
	}
}

// NewFanoutExchange create a fanout exchange
func NewFanoutExchange(exchangeName string) *Exchange {
	return &Exchange{
		name:       exchangeName,
		eType:      exchangeTypeFanout,
		routingKey: "",
	}
}

// NewHeadersExchange create a headers exchange, the headerType supports "all" and "any"
func NewHeadersExchange(exchangeName string, headersType HeadersType, keys map[string]interface{}) *Exchange {
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

func (o *CustomerDeadLetterOptions) isEnabled() bool {
	if o.exchangeName != "" {
		return true
	}
	return false
}

func defaultCustomerDeadLetterOptions() *CustomerDeadLetterOptions {
	return &CustomerDeadLetterOptions{
		exchangeName:       "exchange",
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
