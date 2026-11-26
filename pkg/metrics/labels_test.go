package metrics

import "testing"

// TestLabelValueFromKey 精确 label 解析
func TestLabelValueFromKey(t *testing.T) {
	cases := []struct {
		key, label, want string
	}{
		{"result=error", "result", "error"},
		{"op=Get,result=ok", "result", "ok"},
		{"reason=error_timeout,result=ok", "result", "ok"},  // 关键：reason 含 error 但 result=ok
		{"result=error_timeout", "result", "error_timeout"}, // 区分 error 与 error_timeout
		{"op=Get", "result", ""},                            // 无 result label
	}
	for _, c := range cases {
		if got := labelValueFromKey(c.key, c.label); got != c.want {
			t.Errorf("labelValueFromKey(%q, %q) = %q, want %q", c.key, c.label, got, c.want)
		}
	}
}

// TestIsErrorLabels 精确 error 判定：只认 result=error，不误判
func TestIsErrorLabels(t *testing.T) {
	if !isErrorLabels("result=error") {
		t.Error("result=error 应为 error")
	}
	if !isErrorLabels("op=Get,result=error") {
		t.Error("多 label 的 result=error 应为 error")
	}
	if isErrorLabels("reason=error_timeout,result=ok") {
		t.Error("reason 含 error 但 result=ok 不应误判")
	}
	// 口径演进：result 为未知值（error_timeout/timeout/partial）按失败算——
	// 漏报比误报危险（业务误用 result 值时错误率恒 0、运维完全无感）。
	if !isErrorLabels("result=error_timeout") {
		t.Error("result=error_timeout 应按失败计（非 ok 即失败）")
	}
	if isErrorLabels("result=ok") {
		t.Error("result=ok 不应为 error")
	}
}
