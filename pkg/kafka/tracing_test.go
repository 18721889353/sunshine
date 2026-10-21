package kafka

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// 与 dlock 同因：kafkaTracer = otel.Tracer("kafka") 走全局代理，全局 Provider/Propagator
// 一经首次使用即绑定，逐用例 Set 只首个生效。故用 TestMain 全进程仅设置一次，
// 注入 W3C TraceContext 传播器（生产-消费链路串联依赖它），各用例截取新增 span 断言。
var kafkaRecorder *tracetest.SpanRecorder

func TestMain(m *testing.M) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	kafkaRecorder = sr
	code := m.Run()
	_ = tp.Shutdown(context.Background())
	os.Exit(code)
}

func captureKafkaSpans(t *testing.T, fn func()) tracetest.SpanStubs {
	t.Helper()
	prev := len(kafkaRecorder.Ended())
	fn()
	stubs := make(tracetest.SpanStubs, 0, len(kafkaRecorder.Ended())-prev)
	for _, s := range kafkaRecorder.Ended()[prev:] {
		stubs = append(stubs, tracetest.SpanStubFromReadOnlySpan(s))
	}
	return stubs
}

func assertKafkaAttr(t *testing.T, attrs []attribute.KeyValue, key, expected string) {
	t.Helper()
	for _, a := range attrs {
		if a.Key == attribute.Key(key) {
			assert.Equal(t, expected, a.Value.AsString(), "属性 %s 值不匹配", key)
			return
		}
	}
	t.Errorf("未找到属性 %s", key)
}

// TestKafkaProducerSpanMeta 断言生产 span 名/Kind/semconv 属性符合统一规范。
func TestKafkaProducerSpanMeta(t *testing.T) {
	stubs := captureKafkaSpans(t, func() {
		msg := &sarama.ProducerMessage{Topic: "order-created", Value: sarama.StringEncoder("payload")}
		_, span := startProducerSpan(msg)
		span.End()
	})

	require.Len(t, stubs, 1)
	s := stubs[0]
	assert.Equal(t, "kafka.publish.order-created", s.Name)
	assert.Equal(t, trace.SpanKindProducer, s.SpanKind)
	assertKafkaAttr(t, s.Attributes, "messaging.system", "kafka")
	assertKafkaAttr(t, s.Attributes, "messaging.operation", "publish")
	assertKafkaAttr(t, s.Attributes, "messaging.destination.name", "order-created")
}

// TestKafkaProducerInjectsTraceHeader 验证生产侧向消息头注入 W3C traceparent。
func TestKafkaProducerInjectsTraceHeader(t *testing.T) {
	msg := &sarama.ProducerMessage{Topic: "order-created", Value: sarama.StringEncoder("payload")}
	ctx, span := startProducerSpan(msg)
	injectProducerTrace(ctx, msg)
	span.End()

	var hasTraceparent bool
	for _, h := range msg.Headers {
		if string(h.Key) == "traceparent" {
			hasTraceparent = true
		}
	}
	assert.True(t, hasTraceparent, "生产消息头应注入 traceparent 传播头")
}

// TestKafkaProduceConsumeTraceIDSeries 验证注入-提取闭环：消费 span 与生产 span 同一 traceId。
func TestKafkaProduceConsumeTraceIDSeries(t *testing.T) {
	stubs := captureKafkaSpans(t, func() {
		// 生产端
		pmsg := &sarama.ProducerMessage{Topic: "order-created", Value: sarama.StringEncoder("payload")}
		pctx, pspan := startProducerSpan(pmsg)
		require.True(t, pspan.SpanContext().TraceID().IsValid())
		injectProducerTrace(pctx, pmsg)
		pspan.End()

		// 生产头 []RecordHeader 转消费头 []*RecordHeader
		var cHeaders []*sarama.RecordHeader
		for i := range pmsg.Headers {
			h := pmsg.Headers[i]
			cHeaders = append(cHeaders, &h)
		}

		// 消费端
		cmsg := &sarama.ConsumerMessage{Topic: "order-created", Headers: cHeaders, Partition: 1, Offset: 42, Value: []byte("payload")}
		_, cspan := startConsumerSpan(context.Background(), cmsg)
		cspan.End()
	})

	require.Len(t, stubs, 2)
	assert.Equal(t, trace.SpanKindProducer, stubs[0].SpanKind)
	assert.Equal(t, "kafka.publish.order-created", stubs[0].Name)
	assert.Equal(t, trace.SpanKindConsumer, stubs[1].SpanKind)
	assert.Equal(t, "kafka.receive.order-created", stubs[1].Name)
	assert.Equal(t, stubs[0].SpanContext.TraceID(), stubs[1].SpanContext.TraceID(), "消费 span 应与生产 span 同 traceId 串联")
}

// TestKafkaMarkConsumeResultStatus 验证消费收尾统一口径：成功 OK / 失败 Error。
func TestKafkaMarkConsumeResultStatus(t *testing.T) {
	cmsg := &sarama.ConsumerMessage{Topic: "t", Partition: 0, Offset: 1, Value: []byte("v")}

	stubs := captureKafkaSpans(t, func() {
		_, okSpan := startConsumerSpan(context.Background(), cmsg)
		markConsumeResult(okSpan, nil)
		okSpan.End()

		_, errSpan := startConsumerSpan(context.Background(), cmsg)
		markConsumeResult(errSpan, errors.New("handler boom"))
		errSpan.End()
	})

	require.Len(t, stubs, 2)
	assert.Equal(t, "Ok", stubs[0].Status.Code.String(), "成功消费应标 OK")
	assert.Equal(t, "Error", stubs[1].Status.Code.String(), "失败消费应标 Error")
	assert.Equal(t, "handler boom", stubs[1].Status.Description)
}
