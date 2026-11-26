package metrics

// 韧性指标：超时与重试——系统劣化的早期信号。
// 与 panic 指标一样集中定义，保证诊断引擎按统一指标名聚合。
var (
	requestTimeout = Counter(
		"request_timeout_total",
		"Request timeouts by component and result.",
		[]string{"component", labelResult}...,
	)
	requestRetry = Counter(
		"request_retry_total",
		"Request retries by component.",
		// result=error 必须带：诊断引擎的 Counter 判定靠 isErrorLabels(result!=ok)
		// 识别失败。没有这个 label，重试指标永远不告警（埋了等于白埋）。
		[]string{"component", labelResult}...,
	)
)

// ObserveTimeout 记录一次请求超时。component 为来源（grpc-unary-client / gin-http 等）。
// 超时即故障，统一记 result=error。
func ObserveTimeout(component string) {
	requestTimeout.WithLabelValues(component, labelError).Inc()
}

// ObserveRetry 记录一次重试。component 为来源。
// 重试是"首次失败后的再次尝试"，语义上就是 error 事件——记 result=error 进诊断。
func ObserveRetry(component string) {
	requestRetry.WithLabelValues(component, labelError).Inc()
}
