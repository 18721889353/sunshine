package kafka

import (
	"context"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/IBM/sarama"
)

// ---------------------------------- consume group---------------------------------------

// ConsumerGroup consume group
type ConsumerGroup struct {
	Group            sarama.ConsumerGroup
	groupID          string
	autoCommitEnable bool
}

// InitConsumerGroup init consumer group
func InitConsumerGroup(addrs []string, groupID string, opts ...ConsumerOption) (*ConsumerGroup, error) {
	o := defaultConsumerOptions()
	o.apply(opts...)

	var config *sarama.Config
	if o.config != nil {
		config = o.config
	} else {
		config = sarama.NewConfig()
		config.Version = o.version
		config.Consumer.Group.Rebalance.GroupStrategies = o.groupStrategies
		config.Consumer.Offsets.Initial = o.offsetsInitial
		config.Consumer.Offsets.AutoCommit.Enable = o.offsetsAutoCommitEnable
		config.Consumer.Offsets.AutoCommit.Interval = o.offsetsAutoCommitInterval
		config.ClientID = o.clientID
		if o.tlsConfig != nil {
			config.Net.TLS.Config = o.tlsConfig
			config.Net.TLS.Enable = true
		}
	}

	consumer, err := sarama.NewConsumerGroup(addrs, groupID, config)
	if err != nil {
		return nil, err
	}
	return &ConsumerGroup{
		Group:            consumer,
		groupID:          groupID,
		autoCommitEnable: config.Consumer.Offsets.AutoCommit.Enable,
	}, nil
}

// Consume consume messages
func (c *ConsumerGroup) Consume(ctx context.Context, topics []string, handleMessageFn HandleMessageFn) error {
	handler := &defaultConsumerHandler{
		ctx:              ctx,
		handleMessageFn:  handleMessageFn,
		autoCommitEnable: c.autoCommitEnable,
	}

	err := c.Group.Consume(ctx, topics, handler)
	if err != nil {
		logger.ErrorWithCtx(ctx, "failed to consume messages",
			logger.String("group_id", c.groupID),
			logger.Any("topics", topics),
			logger.Err(err))
		return err
	}
	return nil
}

// ConsumeCustom consume messages for custom handler, you need to implement the sarama.ConsumerGroupHandler interface
func (c *ConsumerGroup) ConsumeCustom(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error {
	err := c.Group.Consume(ctx, topics, handler)
	if err != nil {
		logger.ErrorWithCtx(ctx, "failed to consume messages",
			logger.String("group_id", c.groupID),
			logger.Any("topics", topics),
			logger.Err(err))
		return err
	}
	return nil
}

func (c *ConsumerGroup) Close() error {
	if c == nil || c.Group == nil {
		return c.Group.Close()
	}
	return nil
}

type defaultConsumerHandler struct {
	ctx              context.Context
	handleMessageFn  HandleMessageFn
	autoCommitEnable bool
}

// Setup is run at the beginning of a new session, before ConsumeClaim
func (h *defaultConsumerHandler) Setup(sess sarama.ConsumerGroupSession) error {
	logger.InfoWithCtx(h.ctx, "consumer group session [setup]", logger.Any("claims", sess.Claims()))
	return nil
}

// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
func (h *defaultConsumerHandler) Cleanup(sess sarama.ConsumerGroupSession) error {
	logger.InfoWithCtx(h.ctx, "consumer group session [cleanup]", logger.Any("claims", sess.Claims()))
	return nil
}

// ConsumeClaim consumes messages
func (h *defaultConsumerHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	defer func() {
		if e := recover(); e != nil {
			logger.ErrorWithCtx(h.ctx, "panic occurred while consuming messages", logger.Any("error", e))
			_ = h.ConsumeClaim(sess, claim)
		}
	}()

	for {
		select {
		case <-h.ctx.Done():
			return nil
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			err := h.handleMessageFn(msg)
			if err != nil {
				logger.ErrorWithCtx(h.ctx, "failed to handle message", logger.Err(err))
				continue
			}
			sess.MarkMessage(msg, "")
			if !h.autoCommitEnable {
				sess.Commit()
			}
		}
	}
}

// ---------------------------------- consume partition------------------------------------

// Consumer consume partition
type Consumer struct {
	C sarama.Consumer
}

// InitConsumer init consumer
func InitConsumer(addrs []string, opts ...ConsumerOption) (*Consumer, error) {
	o := defaultConsumerOptions()
	o.apply(opts...)

	var config *sarama.Config
	if o.config != nil {
		config = o.config
	} else {
		config = sarama.NewConfig()
		config.Version = o.version
		config.Consumer.Return.Errors = true
		config.ClientID = o.clientID
		if o.tlsConfig != nil {
			config.Net.TLS.Config = o.tlsConfig
			config.Net.TLS.Enable = true
		}
	}

	consumer, err := sarama.NewConsumer(addrs, config)
	if err != nil {
		return nil, err
	}

	return &Consumer{
		C: consumer,
	}, nil
}

// ConsumePartition consumer one partition, blocking
func (c *Consumer) ConsumePartition(ctx context.Context, topic string, partition int32, offset int64, handleFn HandleMessageFn) {
	defer func() {
		if e := recover(); e != nil {
			logger.ErrorWithCtx(ctx, "panic occurred while consuming messages", logger.Any("error", e))
			c.ConsumePartition(ctx, topic, partition, offset, handleFn)
		}
	}()

	pc, err := c.C.ConsumePartition(topic, partition, offset)
	if err != nil {
		logger.ErrorWithCtx(ctx, "failed to create partition consumer",
			logger.Err(err),
			logger.String("topic", topic),
			logger.Int32("partition", partition))
		return
	}

	logger.InfoWithCtx(ctx, "start consuming partition",
		logger.String("topic", topic),
		logger.Int32("partition", partition),
		logger.Int64("offset", offset))

	for {
		select {
		case msg := <-pc.Messages():
			err = handleFn(msg)
			if err != nil {
				logger.WarnWithCtx(ctx, "failed to handle message",
					logger.Err(err),
					logger.String("topic", topic),
					logger.Int32("partition", partition),
					logger.Int64("offset", msg.Offset))
			}
		case err := <-pc.Errors():
			logger.ErrorWithCtx(ctx, "partition consumer error", logger.Any("err", err))
		case <-ctx.Done():
			return
		}
	}
}

// ConsumeAllPartition consumer all partitions, no blocking
func (c *Consumer) ConsumeAllPartition(ctx context.Context, topic string, offset int64, handleFn HandleMessageFn) {
	partitionList, err := c.C.Partitions(topic)
	if err != nil {
		logger.ErrorWithCtx(ctx, "failed to get partition", logger.Err(err))
		return
	}

	for _, partition := range partitionList {
		go func(partition int32, offset int64) {
			c.ConsumePartition(ctx, topic, partition, offset, handleFn)
		}(partition, offset)
	}
}

// Close the consumer
func (c *Consumer) Close() error {
	if c == nil || c.C == nil {
		return c.C.Close()
	}
	return nil
}
