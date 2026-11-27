package goemail

import (
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// emailSendDuration 外呼邮件耗时（provider, result）。
//
// 为什么必须有：邮件是业务关键链路（通知、账单），
// 发送失败业务无感，只有指标能暴露"外呼通道挂了"（与短信同理）。
var emailSendDuration = pkgmetrics.HistogramDefault("email_send_duration_seconds", "Email send latency by provider and result.", []string{"provider", "result"}...)

// observeEmailSend 记录一次邮件发送耗时。
// err 由调用方按"网络错误 || 业务失败"口径归一（与返回语义一致）。
func observeEmailSend(provider string, err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	emailSendDuration.WithLabelValues(provider, result).Observe(cost.Seconds())
}
