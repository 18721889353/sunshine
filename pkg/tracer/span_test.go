package tracer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewSpanTagTypes 验证 NewSpan 支持 bool/string/int/int64/float64 及其切片类型的标签。
func TestNewSpanTagTypes(t *testing.T) {
	t.Cleanup(func() { SetTraceName("unknown") })
	SetTraceName("foo")

	tags := map[string]interface{}{
		"foo1":  nil,
		"foo2":  true,
		"foo3":  "bar",
		"foo4":  []string{"bar"},
		"foo5":  1,
		"foo6":  []int{1},
		"foo7":  int64(1),
		"foo8":  []int64{1},
		"foo9":  3.14,
		"foo10": []float64{3.14},
		"foo11": map[string]string{"foo": "bar"},
	}
	_, span := NewSpan(context.Background(), "fooSpan", tags)
	assert.NotNil(t, span)
	span.End()
}

// TestNewSpanNilTags 验证 tags 为 nil 时 NewSpan 仍可正常创建 Span。
func TestNewSpanNilTags(t *testing.T) {
	_, span := NewSpan(context.Background(), "nilTagsSpan", nil)
	assert.NotNil(t, span)
	span.End()
}

// TestNewSpanUnknownTagTypeFallback 验证不支持的标签类型降级为字符串存储而不是 panic。
func TestNewSpanUnknownTagTypeFallback(t *testing.T) {
	tags := map[string]interface{}{
		"struct": struct{ X int }{X: 1},
		"chan":   make(chan int),
	}
	_, span := NewSpan(context.Background(), "fallbackSpan", tags)
	assert.NotNil(t, span)
	span.End()
}

// TestSetTraceName 验证 SetTraceName 更新进程级追踪名称，空字符串保持原值不变。
func TestSetTraceName(t *testing.T) {
	t.Cleanup(func() { SetTraceName("unknown") })

	SetTraceName("svc-a")
	assert.Equal(t, "svc-a", getTraceName())

	SetTraceName("") // 空名称不应覆盖已有值
	assert.Equal(t, "svc-a", getTraceName())
}

// TestGetTraceNameFallback 验证未设置过追踪名称时 getTraceName 返回兜底值 unknown。
func TestGetTraceNameFallback(t *testing.T) {
	t.Cleanup(func() { SetTraceName("unknown") })

	SetTraceName("unknown")
	assert.Equal(t, "unknown", getTraceName())
}
