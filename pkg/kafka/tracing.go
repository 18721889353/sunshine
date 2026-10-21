package kafka

// kafka 埋点统一走本文件，遵循 pkg/tracer/status.go 定义的全库口径：
// tracer scope 为单 token "kafka"；span 名 kafka.publish.<topic> / kafka.receive.<topic>；
// 生产 SpanKindProducer、消费 SpanKindConsumer；状态成功 MarkOK、失败 MarkErr；
// 跨进程链路通过 Kafka 消息头注入/提取 W3C Trace Context 关联。

import (
	"context"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/tracer"
)

// kafkaTracer 是 kafka 包唯一 tracer scope（大厂规范：包名单一小写 token，禁止多级点分）。
var kafkaTracer = otel.Tracer("kafka")

// producerHeaderCarrier 适配生产消息头（[]sarama.RecordHeader，Key 为 []byte）到
// OTel TextMapCarrier，用于把 trace 上下文注入 Kafka 消息头。
type producerHeaderCarrier struct {
	headers *[]sarama.RecordHeader
}

func (c producerHeaderCarrier) Get(key string) string {
	for _, h := range *c.headers {
		if string(h.Key) == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c producerHeaderCarrier) Set(key, value string) {
	*c.headers = append(*c.headers, sarama.RecordHeader{Key: []byte(key), Value: []byte(value)})
}

func (c producerHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.headers))
	for _, h := range *c.headers {
		keys = append(keys, string(h.Key))
	}
	return keys
}

// consumerHeaderCarrier 适配消费消息头（[]*sarama.RecordHeader，Key 为 []byte）到
// TextMapCarrier，用于从 Kafka 消息头提取上游链路。消费侧只读，Set 空实现以满足接口。
type consumerHeaderCarrier struct {
	headers []*sarama.RecordHeader
}

func (c consumerHeaderCarrier) Get(key string) string {
	for _, h := range c.headers {
		if h != nil && string(h.Key) == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c consumerHeaderCarrier) Set(string, string) {}

func (c consumerHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for _, h := range c.headers {
		if h != nil {
			keys = append(keys, string(h.Key))
		}
	}
	return keys
}

// injectProducerTrace 将 ctx 的 trace 传播头注入生产消息头，供下游消费者恢复链路。
func injectProducerTrace(ctx context.Context, msg *sarama.ProducerMessage) {
	otel.GetTextMapPropagator().Inject(ctx, producerHeaderCarrier{headers: &msg.Headers})
}

// startProducerSpan 为生产动作起 span。
// 限制：sarama v1.45.2 的 ProducerMessage 无 ctx 字段/GetContext 方法，且导出方法签名
// 不带 context（不改签名以免影响调用方），故以 context.Background() 为根，形成“每条消息
// 一个 trace”；生产 span 向消息头注入 W3C 上下文，消费端 Extract 后与生产同 traceId 串联。
func startProducerSpan(msg *sarama.ProducerMessage) (context.Context, trace.Span) {
	ctx := context.Background()
	size := 0
	if msg.Value != nil {
		size = msg.Value.Length()
	}
	return kafkaTracer.Start(ctx, "kafka.publish."+msg.Topic,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.operation", "publish"),
			attribute.String("messaging.destination.name", msg.Topic),
			attribute.Int("messaging.message.body.size", size),
		))
}

// startConsumerSpan 从消息头恢复上游链路并起消费 span（SpanKindConsumer）。
// parentCtx 为调用方上下文（如 handler/会话 ctx），Extract 后生产-消费同一 traceId 串联。
func startConsumerSpan(parentCtx context.Context, msg *sarama.ConsumerMessage) (context.Context, trace.Span) {
	ctx := otel.GetTextMapPropagator().Extract(parentCtx, consumerHeaderCarrier{headers: msg.Headers})
	return kafkaTracer.Start(ctx, "kafka.receive."+msg.Topic,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.operation", "receive"),
			attribute.String("messaging.destination.name", msg.Topic),
			attribute.Int("messaging.kafka.partition", int(msg.Partition)),
			attribute.Int64("messaging.kafka.offset", msg.Offset),
			attribute.Int("messaging.message.body.size", len(msg.Value)),
		))
}

// markConsumeResult 依据 handler 返回值为消费 span 收尾（统一口径：成功 OK / 失败 Error）。
func markConsumeResult(span trace.Span, err error) {
	if err != nil {
		tracer.MarkErr(span, err)
		return
	}
	tracer.MarkOK(span)
}
