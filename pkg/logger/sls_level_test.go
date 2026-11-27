package logger

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

// TestSLSHook_ShouldSend 级别过滤：warn+ 上报，info/debug 只留本地。
func TestSLSHook_ShouldSend(t *testing.T) {
	cases := []struct {
		minCfg string
		level  zapcore.Level
		want   bool
	}{
		{"", zapcore.InfoLevel, false},      // 默认：info 不报
		{"", zapcore.WarnLevel, true},       // 默认：warn 报
		{"", zapcore.ErrorLevel, true},      // 默认：error 报
		{"", zapcore.DebugLevel, false},     // 默认：debug 不报
		{"info", zapcore.InfoLevel, true},   // 配置 info：info 也报
		{"error", zapcore.WarnLevel, false}, // 配置 error：warn 不报
		{"error", zapcore.ErrorLevel, true},
		{"bogus", zapcore.WarnLevel, true}, // 非法配置回落 warn
		{"bogus", zapcore.InfoLevel, false},
	}
	for _, c := range cases {
		h := &SLSHook{config: &SLSConfig{MinLevel: c.minCfg}}
		if got := h.shouldSend(c.level); got != c.want {
			t.Errorf("min=%q level=%v: got %v want %v", c.minCfg, c.level, got, c.want)
		}
	}
}

// TestNewSLSHook_NilConfig nil 配置应 fail-fast 而非 panic。
func TestNewSLSHook_NilConfig(t *testing.T) {
	_, err := NewSLSHook(nil)
	if err == nil {
		t.Error("nil config 应返回错误")
	}
}

// TestNewSLSHook_InvalidMinLevel 无效级别应 fail-fast（不静默回落）。
func TestNewSLSHook_InvalidMinLevel(t *testing.T) {
	_, err := NewSLSHook(&SLSConfig{MinLevel: "verbose"})
	if err == nil {
		t.Error("无效 minLevel 应返回错误（静默回落会让配置错误无人知晓）")
	}
}

// TestSLSHook_ShouldSend_NilConfig 无 config 时回落 warn（防御性，不 panic）。
func TestSLSHook_ShouldSend_NilConfig(t *testing.T) {
	h := &SLSHook{}
	if !h.shouldSend(zapcore.WarnLevel) {
		t.Error("nil config 应默认放行 warn")
	}
	if h.shouldSend(zapcore.InfoLevel) {
		t.Error("nil config 应过滤 info")
	}
}
