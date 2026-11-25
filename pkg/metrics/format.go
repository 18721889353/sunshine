package metrics

import (
	"fmt"
	"time"
)

// FormatDuration 把秒渲染成人类可读毫秒/秒（单点展示用：端到端 P99、最慢副本）。
func FormatDuration(sec float64) string {
	switch {
	case sec >= 1:
		return fmt.Sprintf("%.2fs", sec)
	case sec >= 0.001:
		return fmt.Sprintf("%.0fms", sec*1000)
	default:
		return fmt.Sprintf("%.1fms", sec*1000)
	}
}

// FormatMs 把秒渲染成毫秒（固定 1 位小数），供耗时行整行统一单位。
// 所有耗时行共用此刻度，保证同屏单位/精度一致。
func FormatMs(sec float64) string {
	return fmt.Sprintf("%.1fms", sec*1000)
}

// FormatIntervalText 渲染窗口标签（距上次成功推送的实际时长）。
//
// 恰为整分钟显示"近N分钟"，否则"近N秒"。实际窗口 = 配置间隔 + 采集/推送耗时，
// 几乎不可能精确整除，故 600s 配置通常显示"近600秒"（不是"近10分钟"）——
// 这是正常现象，非窗口计算错误。
func FormatIntervalText(interval time.Duration) string {
	if interval < time.Minute {
		return fmt.Sprintf("近%d秒", int(interval/time.Second))
	}
	if interval%time.Minute == 0 {
		return fmt.Sprintf("近%d分钟", int(interval/time.Minute))
	}
	return fmt.Sprintf("近%d秒", int(interval/time.Second))
}

// FormatBytes 把字节数渲染成人类可读（KB/MB/GB）。
func FormatBytes(b float64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%.0fB", b)
	}
	div, exp := float64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", b/div, "KMGTPE"[exp])
}
