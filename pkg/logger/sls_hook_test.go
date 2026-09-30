package logger

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"
)

// newValidSLSConfig 返回一份通过 validateSLSConfig 的基线配置，供各用例在其上做单字段破坏。
func newValidSLSConfig() *SLSConfig {
	return &SLSConfig{
		Endpoint:            "cn-hangzhou.log.aliyuncs.com",
		AccessKeyID:         "test-id",
		AccessKeySecret:     "test-secret",
		ProjectName:         "test-project",
		LogStoreName:        "test-logstore",
		MaxRetries:          10,
		Timeout:             60,
		HealthCheckInterval: 30,
		SendTimeout:         5,
	}
}

// TestValidateSLSConfig 验证 SLS 配置边界校验：合法配置通过、各非法项均返回错误。
func TestValidateSLSConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *SLSConfig)
		wantOK bool
	}{
		{"全部合法", func(c *SLSConfig) {}, true},
		{"endpoint 为空", func(c *SLSConfig) { c.Endpoint = "" }, false},
		{"accessKeyId 为空", func(c *SLSConfig) { c.AccessKeyID = "" }, false},
		{"accessKeySecret 为空", func(c *SLSConfig) { c.AccessKeySecret = "" }, false},
		{"projectName 为空", func(c *SLSConfig) { c.ProjectName = "" }, false},
		{"logStoreName 为空", func(c *SLSConfig) { c.LogStoreName = "" }, false},
		{"endpoint 格式非法", func(c *SLSConfig) { c.Endpoint = "not-aliyuncs" }, false},
		{"maxRetries 负值", func(c *SLSConfig) { c.MaxRetries = -1 }, false},
		{"maxRetries 超上限", func(c *SLSConfig) { c.MaxRetries = 101 }, false},
		{"timeout 小于 1", func(c *SLSConfig) { c.Timeout = 0 }, false},
		{"timeout 超上限", func(c *SLSConfig) { c.Timeout = 301 }, false},
		{"healthCheckInterval 过小", func(c *SLSConfig) { c.HealthCheckInterval = 4 }, false},
		{"sendTimeout 小于 1", func(c *SLSConfig) { c.SendTimeout = 0 }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newValidSLSConfig()
			tt.mutate(cfg)
			err := validateSLSConfig(cfg)
			if tt.wantOK && err != nil {
				t.Fatalf("期望合法，实际返回错误: %v", err)
			}
			if !tt.wantOK && err == nil {
				t.Fatalf("期望返回错误，实际为 nil")
			}
		})
	}
}

// TestIsValidEndpoint 验证 Endpoint 后缀白名单：公网/内网/VPC 三种合法后缀，其余拒绝。
func TestIsValidEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     bool
	}{
		{"公网", "cn-hangzhou.log.aliyuncs.com", true},
		{"内网", "cn-hangzhou-intranet.log.aliyuncs.com", true},
		{"VPC", "cn-hangzhou-vpc.log.aliyuncs.com", true},
		{"裸后缀（长度等于后缀，非法）", ".log.aliyuncs.com", false},
		{"错误域名", "cn-hangzhou.log.example.com", false},
		{"空串", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidEndpoint(tt.endpoint); got != tt.want {
				t.Errorf("isValidEndpoint(%q) = %v, 期望 %v", tt.endpoint, got, tt.want)
			}
		})
	}
}

// TestSlsFieldValue 覆盖 zapcore.Field 到 SLS 字符串值的各类型序列化分支，
// 其中浮点分支锁死历史 Bug：zap 用 Float64bits/Float32bits 把浮点位数模式存入 Integer，
// 旧代码直接 float64(field.Integer) 会得到完全错误的数字。
func TestSlsFieldValue(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		want  string
	}{
		{"字符串", String("k", "hello"), "hello"},
		{"整数", Int("k", 42), "42"},
		{"Int64", Int64("k", 9007199254740993), "9007199254740993"},
		{"无符号", Uint("k", 404), "404"},
		{"布尔真", Bool("k", true), "true"},
		{"布尔假", Bool("k", false), "false"},
		{"浮点64", Float64("pi", 3.14159), "3.14159"},
		{"浮点64负值", Float64("neg", -0.5), "-0.5"},
		{"任意类型走JSON", Any("obj", map[string]int{"a": 1}), `{"a":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slsFieldValue(tt.field); got != tt.want {
				t.Errorf("slsFieldValue = %q, 期望 %q", got, tt.want)
			}
		})
	}

	// Float32 无专用封装构造器，直接按 zap 的位编码手工构造，确保按位还原正确。
	t.Run("浮点32按位还原", func(t *testing.T) {
		f := zapcore.Field{
			Key:     "f32",
			Type:    zapcore.Float32Type,
			Integer: int64(math.Float32bits(float32(1.5))),
		}
		if got := slsFieldValue(f); got != "1.5" {
			t.Errorf("Float32 slsFieldValue = %q, 期望 %q", got, "1.5")
		}
	})

	// 显式反证历史 Bug 写法：直接 float64(Integer) 与正确值必然不同。
	t.Run("反证历史错误写法", func(t *testing.T) {
		f := Float64("x", 3.14)
		wrong := strconv.FormatFloat(float64(f.Integer), 'f', -1, 64)
		if wrong == slsFieldValue(f) {
			t.Errorf("错误写法与修复结果相同，用例失效: %q", wrong)
		}
	})
}

// FuzzValidateSLSConfig 守护不变量：任意配置输入都不 panic，且合法/非法判定自洽
// （返回 nil 时各必填字段与边界必须都满足）。
func FuzzValidateSLSConfig(f *testing.F) {
	seeds := []struct {
		ep, ak, sk, pj, ls       string
		retries, timeout, hc, st int
	}{
		{"cn-hangzhou.log.aliyuncs.com", "id", "sec", "p", "ls", 10, 60, 30, 5},
		{"", "", "", "", "", 0, 0, 0, 0},
		{"evil\x00endpoint", "i", "s", "p", "l", 999, -5, 1, 0},
	}
	for _, s := range seeds {
		f.Add(s.ep, s.ak, s.sk, s.pj, s.ls, s.retries, s.timeout, s.hc, s.st)
	}

	f.Fuzz(func(t *testing.T, ep, ak, sk, pj, ls string, retries, timeout, hc, st int) {
		cfg := &SLSConfig{
			Endpoint: ep, AccessKeyID: ak, AccessKeySecret: sk,
			ProjectName: pj, LogStoreName: ls,
			MaxRetries: retries, Timeout: timeout,
			HealthCheckInterval: hc, SendTimeout: st,
		}
		err := validateSLSConfig(cfg)
		if err == nil {
			// 通过校验时反向断言：必填非空、数值在合法区间内
			if ep == "" || ak == "" || sk == "" || pj == "" || ls == "" {
				t.Fatalf("校验通过但存在空的必填字段: %+v", cfg)
			}
			if retries < 0 || retries > 100 || timeout < 1 || timeout > 300 || hc < 5 || st < 1 {
				t.Fatalf("校验通过但数值越界: retries=%d timeout=%d hc=%d st=%d", retries, timeout, hc, st)
			}
		}
	})
}

// FuzzSlsFieldValue 守护不变量：任意字段输入都不 panic，且返回值为合法字符串。
func FuzzSlsFieldValue(f *testing.F) {
	f.Add("k", "v", int64(0), int8(zapcore.StringType))
	f.Add("k", "", int64(math.Float64bits(3.14)), int8(zapcore.Float64Type))
	f.Add("k", "", int64(math.Float32bits(1.5)), int8(zapcore.Float32Type))
	f.Add("k", "", int64(-9223372036854775808), int8(zapcore.Int64Type))

	f.Fuzz(func(t *testing.T, key, str string, integer int64, typeCode int8) {
		field := Field{Key: key, String: str, Integer: integer, Type: zapcore.FieldType(typeCode)}
		got := slsFieldValue(field)
		// 数字类型结果必须可被 strconv 解析回数值（保证是规范数字而非乱码）
		switch field.Type {
		case zapcore.Int64Type, zapcore.Int32Type:
			if _, err := strconv.ParseInt(got, 10, 64); err != nil {
				t.Fatalf("Int 类型序列化结果无法解析: %q err=%v", got, err)
			}
		case zapcore.Float64Type:
			if _, err := strconv.ParseFloat(got, 64); err != nil {
				t.Fatalf("Float64 序列化结果无法解析: %q err=%v", got, err)
			}
		}
		if strings.ContainsRune(got, '\n') && field.Type == zapcore.Int64Type {
			t.Fatalf("数字类型序列化结果不应含换行: %q", got)
		}
	})
}
