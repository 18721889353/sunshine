package gosms

import (
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// smsSendDuration 外呼短信耗时（provider, result）。
//
// 为什么必须有：短信/邮件外呼是业务关键链路（验证码、通知），
// 发送失败业务无感（用户收不到码），只有指标能暴露"外呼通道挂了"。
var smsSendDuration = pkgmetrics.HistogramDefault("sms_send_duration_seconds", "SMS send latency by provider and result.", []string{"provider", "result"}...)

// observeSMSSend 记录一次短信发送耗时。
// err 由调用方按"网络错误 || 业务失败"口径归一（与返回语义一致）。
func observeSMSSend(err error, cost time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	smsSendDuration.WithLabelValues("aliyun", result).Observe(cost.Seconds())
}
