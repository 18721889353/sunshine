package metrics

// 保护机制指标：限流/熔断拦截——容量与下游故障的第一信号。
//
// 为什么必须有：限流拒绝、熔断打开是"系统过载/下游故障"的直接表现，
// 但请求侧只有 429/503 状态码（散落在 http_request_count 的 status label 里），
// 无法一眼看出"保护机制触发了多少次"。单独计数才能回答：
// "刚才那波流量，限流挡了多少？熔断开了几次？"
var sentinelBlock = Counter(
	"sentinel_block_total",
	"Requests blocked by rate limiting or circuit breaking by type.",
	[]string{"type", labelResult}...,
)

// ObserveSentinelBlock 记录一次被保护机制拦截的请求。
// blockType: "flow"（限流）/ "circuit_breaking"（熔断）。
// result 固定 error：被拦截的请求对调用方是失败（429/503），
// 进 Counter 诊断（result!=ok 即告警）。
func ObserveSentinelBlock(blockType string) {
	sentinelBlock.WithLabelValues(blockType, labelError).Inc()
}
