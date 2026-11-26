package kafka

import (
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// kafka 指标：对齐 rabbitmq 的 publish/consume 模式（topic 维度）。
//
// 为什么必须有：消息中间件是典型的"静默故障"高发区——
// 生产失败/消费卡住时业务无感，只有指标能暴露。
// 之前 rabbitmq 有完整指标而 kafka 完全没有，覆盖不一致。
var (
	kafkaPublishDuration = pkgmetrics.HistogramDefault("kafka_publish_duration_seconds", "Kafka publish latency by topic and result.", []string{"topic", "result"}...)

	kafkaConsumeDuration = pkgmetrics.HistogramDefault("kafka_consume_duration_seconds", "Kafka message handling latency by topic and result.", []string{"topic", "result"}...)
)

// observeKafkaPublish 记录一次生产耗时（统一 result 口径）。
func observeKafkaPublish(topic string, err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	kafkaPublishDuration.WithLabelValues(topic, result).Observe(cost.Seconds())
}

// observeKafkaConsume 记录一次消息处理耗时。
func observeKafkaConsume(topic string, err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	kafkaConsumeDuration.WithLabelValues(topic, result).Observe(cost.Seconds())
}

// observeKafkaPanic 记录消费/生产链路的 panic 恢复（复用公共 panic 指标）。
func observeKafkaPanic(component string) {
	pkgmetrics.ObservePanic(component)
}
