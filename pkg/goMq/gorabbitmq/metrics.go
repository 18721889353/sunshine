package gorabbitmq

import (
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// rabbitmq 指标：发布与消费分别计数（exchange/queue 维度）。
// 埋点在 Producer.publish 与 Consumer.safeHandle 统一入口，业务零改动。
var (
	rabbitmqPublishDuration = pkgmetrics.HistogramDefault("rabbitmq_publish_duration_seconds", "RabbitMQ publish latency by exchange and result.", []string{"exchange", "result"}...)

	rabbitmqConsumeDuration = pkgmetrics.HistogramDefault("rabbitmq_consume_duration_seconds", "RabbitMQ consumer handler latency by queue and result.", []string{"queue", "result"}...)
)

// observeRabbitmqPublish 记录一次发布耗时。
func observeRabbitmqPublish(exchange string, err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	rabbitmqPublishDuration.WithLabelValues(exchange, result).Observe(cost.Seconds())
}

// observeRabbitmqConsume 记录一次消费处理耗时。
func observeRabbitmqConsume(queue string, err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	rabbitmqConsumeDuration.WithLabelValues(queue, result).Observe(cost.Seconds())
}
