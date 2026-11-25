package metrics

import (
	"strings"
	"testing"
)

// TestMaskWebhookURL 遮蔽 key
func TestMaskWebhookURL(t *testing.T) {
	u := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc123def456"
	got := MaskWebhookURL(u)
	if got == u {
		t.Error("key 应被遮蔽")
	}
	if !contains(got, "key=***") {
		t.Errorf("应含 key=***, got %s", got)
	}
	if contains(got, "abc123def456") {
		t.Error("原始 key 不应出现")
	}
}

// TestRedactWebhookError 错误脱敏
func TestRedactWebhookError(t *testing.T) {
	u := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=SECRET"
	err := errFakeWebhook("post to " + u + " failed")
	got := RedactWebhookError(err, u)
	if contains(got, "SECRET") {
		t.Errorf("错误信息不应含 key: %s", got)
	}
}

// TestTruncateUTF8 字节截断保留完整字符
func TestTruncateUTF8(t *testing.T) {
	// 3 字节中文
	s := "中文中文中文"
	got := truncateUTF8(s, 4) // 4 字节 = 1 个中文字 + 1 字节（不完整）
	// 应退到字符边界
	for _, r := range got {
		_ = r
	}
	if len(got) > 6 { // 1 字符(3B) + 省略号(3B)
		t.Errorf("截断过长: %q (%d bytes)", got, len(got))
	}
	// 不超长不截断
	if truncateUTF8("abc", 10) != "abc" {
		t.Error("不超长应原样")
	}
}

// TestIsWeComWebhookHost 域名判断（精确匹配，防后缀伪装绕过）
func TestIsWeComWebhookHost(t *testing.T) {
	for _, host := range []string{
		"qyapi.weixin.qq.com",
		"QYAPI.WEIXIN.QQ.COM",     // 大小写不敏感
		"qyapi.weixin.qq.com.",    // FQDN 尾点
		"sub.qyapi.weixin.qq.com", // 标准子域
	} {
		if !IsWeComWebhookHost(host) {
			t.Errorf("企微域名应识别: %s", host)
		}
	}
	// 变异守护：这些 host 都含 "qyapi.weixin.qq.com" 子串，但都不是企微域名。
	// 如果实现退化回 strings.Contains，本测试立即失败。
	for _, host := range []string{
		"evil.com",
		"qyapi.weixin.qq.com.evil.com", // 后缀伪装
		"evil-qyapi.weixin.qq.com",     // 前缀伪装
		"qyapi.weixin.qq.com@evil.com", // userinfo 伪装
		"notqyapi.weixin.qq.com",       // 前缀粘连
	} {
		if IsWeComWebhookHost(host) {
			t.Errorf("外部域名不应识别（含子串不等于企微域名）: %s", host)
		}
	}
}

type fakeErr string

func (e fakeErr) Error() string     { return string(e) }
func errFakeWebhook(s string) error { return fakeErr(s) }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// TestTruncateUTF8_Budget 截断后总字节数不得超过 maxBytes（省略号预留）
func TestTruncateUTF8_Budget(t *testing.T) {
	s := strings.Repeat("中", 1000) // 3000 字节
	for _, max := range []int{1800, 100, 10, 4} {
		got := truncateUTF8(s, max)
		if len(got) > max {
			t.Errorf("max=%d, 截断后 %d 字节超限: %q", max, len(got), got)
		}
	}
}
