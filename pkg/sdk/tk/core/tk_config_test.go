package core

import (
	"testing"
	"time"
)

// TestSmoke_NewTkConfig 默认配置 + 选项覆盖生效（不依赖全局态）。
func TestSmoke_NewTkConfig(t *testing.T) {
	c := NewTkConfig()
	if c.HTTPReadTimeout != 10000 {
		t.Errorf("默认超时应为 10000, got %d", c.HTTPReadTimeout)
	}
	if c.OpenRequestURL == "" {
		t.Error("默认 URL 不应为空")
	}
}

// TestSmoke_NewTkConfig_Options 各 option 覆盖字段。
func TestSmoke_NewTkConfig_Options(t *testing.T) {
	c := NewTkConfig(
		WithAppID("app-1"),
		WithAppSecret("secret-1"),
		WithHTTPReadTimeout(3000),
		WithIdleConnTimeout(5*time.Second),
	)
	if c.AppID != "app-1" || c.AppSecret != "secret-1" {
		t.Errorf("选项未生效: app=%q secret=%q", c.AppID, c.AppSecret)
	}
	if c.HTTPReadTimeout != 3000 {
		t.Errorf("超时选项未生效, got %d", c.HTTPReadTimeout)
	}
	if c.IdleConnTimeout != 5*time.Second {
		t.Errorf("IdleConnTimeout 未生效, got %v", c.IdleConnTimeout)
	}
}

// TestSmoke_NewTkAPIClient 客户端构造不 panic。
func TestSmoke_NewTkAPIClient(t *testing.T) {
	if NewTkAPIClient() == nil {
		t.Error("NewTkAPIClient 不应返回 nil")
	}
}
