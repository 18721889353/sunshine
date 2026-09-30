package logger

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap/zapcore"
)

// TestMergeRouteConfig 验证路由配置合并：数值为 0 时继承默认、Format 为空时逐级兜底。
func TestMergeRouteConfig(t *testing.T) {
	base := &RouteConfig{MaxSize: 100, MaxBackups: 30, MaxAge: 7, Format: "console"}

	tests := []struct {
		name  string
		route *RouteConfig
		check func(t *testing.T, m *RouteConfig)
	}{
		{
			name:  "路由显式小值优先于默认",
			route: &RouteConfig{Module: "order", MaxSize: 5},
			check: func(t *testing.T, m *RouteConfig) {
				if m.MaxSize != 5 {
					t.Errorf("MaxSize 应保持 5，实际 %d", m.MaxSize)
				}
			},
		},
		{
			name:  "数值为0继承默认",
			route: &RouteConfig{Module: "order"},
			check: func(t *testing.T, m *RouteConfig) {
				if m.MaxSize != 100 || m.MaxBackups != 30 || m.MaxAge != 7 {
					t.Errorf("未继承默认数值: %+v", m)
				}
			},
		},
		{
			name:  "路由显式数值优先于默认",
			route: &RouteConfig{Module: "order", MaxSize: 200},
			check: func(t *testing.T, m *RouteConfig) {
				if m.MaxSize != 200 {
					t.Errorf("MaxSize 期望 200，实际 %d", m.MaxSize)
				}
			},
		},
		{
			name:  "Format为空继承默认",
			route: &RouteConfig{Module: "order"},
			check: func(t *testing.T, m *RouteConfig) {
				if m.Format != "console" {
					t.Errorf("Format 期望继承 console，实际 %q", m.Format)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, mergeRouteConfig(tt.route, base))
		})
	}

	// 单独验证 Format 双空兜底为 json
	t.Run("Format双空兜底json", func(t *testing.T) {
		emptyDefault := &RouteConfig{Format: ""}
		m := mergeRouteConfig(&RouteConfig{Module: "x"}, emptyDefault)
		if m.Format != "json" {
			t.Errorf("期望兜底 json，实际 %q", m.Format)
		}
	})
}

// TestGetRouteKey 验证路由键生成：Module 优先，其次级别键，最后 default。
func TestGetRouteKey(t *testing.T) {
	r := &LogRouter{}
	tests := []struct {
		name   string
		config *RouteConfig
		want   string
	}{
		{"有模块名", &RouteConfig{Module: "order"}, "order"},
		{"仅有级别", &RouteConfig{Level: "error"}, "level_ERROR"},
		{"模块名优先于级别", &RouteConfig{Module: "order", Level: "error"}, "order"},
		{"两者皆空", &RouteConfig{}, "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.getRouteKey(tt.config); got != tt.want {
				t.Errorf("getRouteKey = %q, 期望 %q", got, tt.want)
			}
		})
	}
}

// TestBuildLogFilePath 验证日志路径构建：空兜底 out.log、绝对/相对路径均自动建目录。
func TestBuildLogFilePath(t *testing.T) {
	t.Run("空文件名兜底out.log", func(t *testing.T) {
		if got, err := buildLogFilePath(""); err != nil || got != "out.log" {
			t.Errorf("期望 out.log，实际 %q (err=%v)", got, err)
		}
	})

	t.Run("绝对路径自动建目录", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nested", "app.log")
		got, err := buildLogFilePath(dir)
		if err != nil || got != dir {
			t.Fatalf("绝对路径应原样返回，期望 %q 实际 %q (err=%v)", dir, got, err)
		}
		if _, err := os.Stat(filepath.Dir(dir)); err != nil {
			t.Errorf("期望自动创建目录，实际不存在: %v", err)
		}
	})

	t.Run("相对带子目录路径自动建目录", func(t *testing.T) {
		runDir := filepath.Join(t.TempDir(), "run")
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			t.Fatalf("创建运行目录失败: %v", err)
		}
		t.Chdir(runDir) // 测试结束自动切回原工作目录，避免 Windows 下 TempDir 清理时目录被占用
		rel := filepath.Join("sub", "x.log")
		if got, err := buildLogFilePath(rel); err != nil || got != rel {
			t.Fatalf("相对路径应原样返回，实际 %q (err=%v)", got, err)
		}
		if _, err := os.Stat("sub"); err != nil {
			t.Errorf("期望自动创建子目录，实际不存在: %v", err)
		}
	})
}

// TestExtractContextFields 验证从 context 提取 request_id / caller_func 字段。
func TestExtractContextFields(t *testing.T) {
	t.Run("nil context 返回空", func(t *testing.T) {
		if got := extractContextFields(nil); got != nil {
			t.Errorf("nil ctx 期望 nil，实际 %v", got)
		}
	})

	t.Run("空 context 无字段", func(t *testing.T) {
		if got := extractContextFields(context.Background()); len(got) != 0 {
			t.Errorf("空 ctx 期望无字段，实际 %d 个", len(got))
		}
	})

	t.Run("含 request_id 与 caller_func", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyRequestID, "req-1")
		ctx = context.WithValue(ctx, ContextKeyCallerFunc, "Service.Method")
		fields := extractContextFields(ctx)
		if len(fields) != 2 {
			t.Fatalf("期望提取 2 个字段，实际 %d", len(fields))
		}
		assertField(t, fields, string(ContextKeyRequestID), "req-1")
		assertField(t, fields, "caller_func", "Service.Method")
	})

	t.Run("request_id 类型不符被忽略", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), ContextKeyRequestID, 12345) // 非 string
		if got := extractContextFields(ctx); len(got) != 0 {
			t.Errorf("非字符串 request_id 期望被忽略，实际 %d 个字段", len(got))
		}
	})
}

// assertField 断言字段列表中存在指定 key/value 的字符串字段。
func assertField(t *testing.T, fields []zapcore.Field, key, wantVal string) {
	t.Helper()
	for _, f := range fields {
		if f.Key == key {
			if f.Type != zapcore.StringType || f.String != wantVal {
				t.Errorf("字段 %q 值期望 %q，实际 type=%v val=%q", key, wantVal, f.Type, f.String)
			}
			return
		}
	}
	t.Errorf("字段列表中缺少 %q", key)
}
