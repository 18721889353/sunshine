package gocron

import (
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// cronTaskDuration 记录定时任务执行耗时（task, result）。
//
// 为什么必须有：定时任务失败是【典型静默故障】——任务挂了业务无感，
// 只有耗时/成功率指标能暴露"任务没跑"或"任务跑挂了"。
// result: ok / error（Fn 无返回值，用 recover 检测 panic，panic 记 error；细节已在日志）。
var cronTaskDuration = pkgmetrics.HistogramDefault("cron_task_duration_seconds", "Cron task execution latency by task name and result.", []string{"task", "result"}...)

// observeCronTask 记录一次任务执行耗时。
// 统一口径：result 为 ok/error（panic 记 error，与全仓一致）。
func observeCronTask(task string, panicked bool, cost time.Duration) {
	result := "ok"
	if panicked {
		result = "error"
	}
	cronTaskDuration.WithLabelValues(task, result).Observe(cost.Seconds())
}
