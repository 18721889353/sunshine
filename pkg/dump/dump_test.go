package dump

import (
	"strings"
	"testing"
)

// TestSmoke_Format 基本值格式化不 panic 且含值内容。
func TestSmoke_Format(t *testing.T) {
	got := Format("hello", 42)
	if !strings.Contains(got, "hello") || !strings.Contains(got, "42") {
		t.Errorf("Format 应含输入值, got %q", got)
	}
}

// TestSmoke_Fprint 写入自定义 writer 不 panic。
func TestSmoke_Fprint(t *testing.T) {
	var buf strings.Builder
	Fprint(&buf, "value", 7)
	if buf.Len() == 0 {
		t.Error("Fprint 应写入内容")
	}
}

// TestSmoke_NewDumper 自定义 Dumper 可独立工作。
func TestSmoke_NewDumper(t *testing.T) {
	var buf strings.Builder
	d := NewDumper(&buf, 2)
	if d == nil {
		t.Fatal("NewDumper 不应返回 nil")
	}
	d.Print("independent")
	if buf.Len() == 0 {
		t.Error("自定义 Dumper 应写入内容")
	}
}
