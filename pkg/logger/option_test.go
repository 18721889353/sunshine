package logger

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

// TestWithLevel 验证级别选项：合法值归一为大写，非法/空值兜底默认 info（而非 debug，防拼写错误导致日志量暴涨）。
func TestWithLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"debug", "debug", levelDebug},
		{"info 大写归一", "INFO", levelInfo},
		{"warn", "warn", levelWarn},
		{"error", "error", levelError},
		{"非法值兜底info", "verbose", defaultLevel},
		{"拼写错误兜底info", "waring", defaultLevel},
		{"空串兜底info", "", defaultLevel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := defaultOptions()
			o.apply(WithLevel(tt.input))
			if o.level != tt.want {
				t.Errorf("level = %q, 期望 %q", o.level, tt.want)
			}
		})
	}
}

// TestWithFormat 验证格式选项：console/json 生效，非法值兜底默认 json。
func TestWithFormat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"console", "console", formatConsole},
		{"json 大写归一", "JSON", formatJSON},
		{"非法值兜底json", "yaml", formatJSON},
		{"空串兜底json", "", formatJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := defaultOptions()
			o.apply(WithFormat(tt.input))
			if o.encoding != tt.want {
				t.Errorf("encoding = %q, 期望 %q", o.encoding, tt.want)
			}
		})
	}
}

// TestApplyNilOption 验证 apply 对 nil Option 的防御：跳过 nil 不 panic，其余正常生效。
func TestApplyNilOption(t *testing.T) {
	o := defaultOptions()
	o.apply(nil, WithLevel("info"), nil, WithFormat("console"))
	if o.level != levelInfo {
		t.Errorf("level 期望 %q，实际 %q", levelInfo, o.level)
	}
	if o.encoding != formatConsole {
		t.Errorf("encoding 期望 %q，实际 %q", formatConsole, o.encoding)
	}
}

// TestWithSave 验证保存开关与文件配置的联动。
func TestWithSave(t *testing.T) {
	t.Run("开启保存生成文件配置", func(t *testing.T) {
		o := defaultOptions()
		o.apply(WithSave(true, WithFileName("custom.log")))
		if !o.isSave {
			t.Fatal("isSave 期望 true")
		}
		if o.fileConfig == nil || o.fileConfig.filename != "custom.log" {
			t.Errorf("期望生成含自定义文件名的 fileConfig，实际 %+v", o.fileConfig)
		}
	})

	t.Run("关闭保存不生成文件配置", func(t *testing.T) {
		o := defaultOptions()
		o.apply(WithSave(false))
		if o.isSave {
			t.Fatal("isSave 期望 false")
		}
		if o.fileConfig != nil {
			t.Errorf("关闭保存时 fileConfig 期望 nil，实际 %+v", o.fileConfig)
		}
	})
}

// TestFileOptionGuards 验证文件选项对非法值的兜底：空名/非正数被忽略并保留默认。
func TestFileOptionGuards(t *testing.T) {
	t.Run("空文件名被忽略", func(t *testing.T) {
		f := defaultFileOptions()
		f.apply(WithFileName(""))
		if f.filename != defaultFilename {
			t.Errorf("空文件名不应覆盖默认，实际 %q", f.filename)
		}
	})

	t.Run("非正数值被忽略", func(t *testing.T) {
		f := defaultFileOptions()
		before := *f
		f.apply(WithFileMaxSize(0), WithFileMaxBackups(-1), WithFileMaxAge(0))
		if f.maxSize != before.maxSize || f.maxBackups != before.maxBackups || f.maxAge != before.maxAge {
			t.Errorf("非法数值不应覆盖默认: %+v", f)
		}
	})

	t.Run("合法值生效", func(t *testing.T) {
		f := defaultFileOptions()
		f.apply(WithFileName("a.log"), WithFileMaxSize(5), WithFileIsCompression(true))
		if f.filename != "a.log" || f.maxSize != 5 || !f.isCompression {
			t.Errorf("合法值未生效: %+v", f)
		}
	})
}

// TestGetLevelSize 验证级别字符串到 zapcore.Level 的映射，未知级别（如 trace）兜底 Info（与 WithLevel 兜底策略一致）。
func TestGetLevelSize(t *testing.T) {
	tests := []struct {
		input string
		want  zapcore.Level
	}{
		{"debug", zapcore.DebugLevel},
		{"info", zapcore.InfoLevel},
		{"warn", zapcore.WarnLevel},
		{"error", zapcore.ErrorLevel},
		{"trace", zapcore.InfoLevel},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := getLevelSize(tt.input); got != tt.want {
				t.Errorf("getLevelSize(%q) = %v, 期望 %v", tt.input, got, tt.want)
			}
		})
	}
}
