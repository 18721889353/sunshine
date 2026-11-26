package metrics

// labelResult 统一 label 名（避免 goconst 在多文件重复告警）。
const (
	labelResult = "result"
	labelError  = "error"
)

// 公共 panic 恢复指标：所有 interceptor / 中间件共享同一个 counter，
// 保证诊断引擎能按统一指标名聚合（component label 区分来源）。
//
// 为什么必须有：panic 是最严重的系统故障，但被 recovery 捕获后进程不挂，
// 只能靠日志发现——没有指标就等于"静默故障"。
var panicRecovered = Counter(
	"panic_recovered_total",
	"Panics recovered by interceptors/middlewares by component.",
	[]string{"component", labelResult}...,
)

// ObservePanic 记录一次被恢复的 panic。component 为来源（grpc-unary-server / gin 等）。
// 统一口径：panic 记 result=error。
func ObservePanic(component string) {
	panicRecovered.WithLabelValues(component, labelError).Inc()
}
