package metrics

import (
	"testing"
)

// TestWithWebhookRoutes_Applied option 应用后路由参与匹配（行为验证，不依赖未导出字段）。
func TestWithWebhookRoutes_Applied(t *testing.T) {
	r := &Reporter{}
	WithWebhookRoutes([]WebhookRoute{
		{Types: []string{ReportTypeAlert}, Levels: []string{LevelKeyWarn}, Urls: []string{"http://x"}},
	})(r)
	if _, ok := r.matchRoute(ReportTypeAlert, DiagnosisWarn); !ok {
		t.Error("option 应用后 alert+warn 应命中路由")
	}
	if _, ok := r.matchRoute(ReportTypePeriodic, DiagnosisOK); ok {
		t.Error("periodic 不在配置中，不应命中")
	}
}
