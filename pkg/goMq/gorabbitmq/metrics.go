package gorabbitmq

import (
	"sync"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"

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

// ==================== 连接状态（复用 dbpool.go 的 Collector 模式）====================
//
// 为什么必须有：连接断开/反复重连是网络或服务端故障的关键信号——
// 进程没挂、日志淹没时，只有指标能暴露"MQ 连接其实一直不稳"。
// rabbitmqQueueMessages 队列积压深度（Gauge，当前值不差分）。
// 积压是"下游跟不上"最直接的信号，比错误率更早暴露问题。
var rabbitmqQueueMessages = prom.NewGaugeVec(prom.GaugeOpts{
	Name: "rabbitmq_queue_messages",
	Help: "Number of messages ready in the RabbitMQ queue (backlog).",
}, []string{"queue"})

// ObserveQueueDepth 记录队列积压深度。
func ObserveQueueDepth(queue string, messages int) {
	rabbitmqQueueMessages.WithLabelValues(queue).Set(float64(messages))
}

var (
	descConnected  = prom.NewDesc("rabbitmq_connected", "RabbitMQ connection state (1=connected, 0=disconnected).", []string{"url"}, nil)
	descReconnects = prom.NewDesc("rabbitmq_reconnect_count_total", "Total RabbitMQ connection reconnect attempts.", []string{"url", "result"}, nil)
)

var (
	connMu       sync.Mutex
	connRegister = map[string]*Connection{}
)

// RegisterConnectionStats 登记 RabbitMQ 连接供指标采集（幂等，重复登记同名忽略）。
// name 用于 label 区分（多连接场景）。应在 NewConnection 成功后调用。
func RegisterConnectionStats(name string, c *Connection) {
	if c == nil {
		return
	}
	connMu.Lock()
	defer connMu.Unlock()
	if _, exists := connRegister[name]; exists {
		return
	}
	connRegister[name] = c
}

func init() {
	pkgmetrics.MustRegister(prom.CollectorFunc(collectRabbitmqConnStats), rabbitmqQueueMessages)
}

func collectRabbitmqConnStats(ch chan<- prom.Metric) {
	connMu.Lock()
	conns := make(map[string]*Connection, len(connRegister))
	for k, v := range connRegister {
		conns[k] = v
	}
	connMu.Unlock()

	for name, c := range conns {
		connected := 0.0
		if c.isConnected.Load() {
			connected = 1.0
		}
		// reconnectCount 是进程内累计值——直接 emit（CounterVec.Add 会每次 Gather 虚高）
		ch <- prom.MustNewConstMetric(descConnected, prom.GaugeValue, connected, name)
		ch <- prom.MustNewConstMetric(descReconnects, prom.CounterValue, float64(c.reconnectCount.Load()), name, "error")
	}
}
