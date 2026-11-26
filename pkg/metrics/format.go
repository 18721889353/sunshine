package metrics

import (
	"fmt"
	"time"
)

// FormatDuration 把秒渲染成人类可读耗时（诊断结论里的 P99 等单点展示）。
// 委托 FormatMs 保持全仓唯一刻度——原实现与 FormatMs 分叉：亚毫秒级显示
// "0.0ms"（精度丢失）、毫秒级少一位小数，同屏结论与报告行单位/精度不一致。
func FormatDuration(sec float64) string {
	return FormatMs(sec)
}

// FormatMs 把秒渲染成毫秒（固定 1 位小数），供耗时行整行统一单位。
// 所有耗时行共用此刻度，保证同屏单位/精度一致。
func FormatMs(sec float64) string {
	// 分级渲染：秒级显示 7.3s 比 7300.0ms 直观（亚毫秒级保留精度）
	switch {
	case sec < 0.001:
		return fmt.Sprintf("%.0fμs", sec*1e6)
	case sec < 1:
		return fmt.Sprintf("%.1fms", sec*1000)
	default:
		return fmt.Sprintf("%.2fs", sec)
	}
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
	const suffixes = "KMGTPE" // 索引 0..5
	if b < unit {
		if b < 0 {
			b = 0 // 字节数不应为负；防御外部异常值，避免 "-5B" 误导
		}
		return fmt.Sprintf("%.0fB", b)
	}
	div, exp := float64(unit), 0
	// exp 上限夹到后缀表末位：超大值（或 float 异常）不能让下标越界 panic
	for n := b / unit; n >= unit && exp < len(suffixes)-1; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", b/div, suffixes[exp])
}
