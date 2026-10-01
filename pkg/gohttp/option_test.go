package gohttp

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestValidateBaseURL 校验 WithBaseURL 的空值、非法协议与缺主机名场景
// （空串合法：等价于不设置基础 URL，请求时传完整地址）
func TestValidateBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantIs  error // 按 errors.Is 断言的哨兵错误
		wantErr bool  // 是否期望普通格式错误
	}{
		{"空字符串合法通过", "", nil, false},
		{"非法协议被拒绝", "ftp://api.example.com", nil, true},
		{"缺少主机名被拒绝", "https:///path", nil, true},
		{"合法 http 通过", "http://api.example.com", nil, false},
		{"合法 https 通过", "https://api.example.com/v1", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := defaultOptions()
			WithBaseURL(tt.baseURL)(o)
			err := o.validate()
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantIs != nil {
					assert.ErrorIs(t, err, tt.wantIs)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestValidateTimeout 校验超时时间的正负边界：负值/零值在构造期快速失败
func TestValidateTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr bool
	}{
		{"负超时被拒绝", -time.Second, true},
		{"零超时被拒绝", 0, true},
		{"正超时通过", time.Second, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := defaultOptions()
			WithTimeout(tt.timeout)(o)
			err := o.validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "WithTimeout 必须为正数")
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestValidateRetry 校验重试参数：负次数/负等待/最大等待小于最小等待均被拒绝
func TestValidateRetry(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		minWait time.Duration
		maxWait time.Duration
		wantErr string
	}{
		{"负重试次数被拒绝", -1, time.Second, time.Second, "重试次数不可为负数"},
		{"负最小等待被拒绝", 3, -time.Second, time.Second, "最小等待时间不可为负数"},
		{"最大等待小于最小等待被拒绝", 3, 2 * time.Second, time.Second, "不可小于最小等待时间"},
		{"边界值全部为零通过", 0, 0, 0, ""},
		{"合法组合通过", 3, time.Second, 5 * time.Second, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := defaultOptions()
			WithRetry(tt.count, tt.minWait, tt.maxWait)(o)
			err := o.validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestValidateTransportNil 校验 WithTransport(nil) 显式误用被快速失败（原实现静默忽略）
func TestValidateTransportNil(t *testing.T) {
	o := defaultOptions()
	WithTransport(nil)(o)
	err := o.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithTransport 不可传入 nil")

	o = defaultOptions()
	WithTransport(&http.Transport{})(o)
	require.NoError(t, o.validate())
}

// TestValidateProxy 校验代理配置：非法协议被拒绝、空串表示清除代理、合法协议通过
func TestValidateProxy(t *testing.T) {
	tests := []struct {
		name    string
		proxy   string
		wantErr bool
	}{
		{"空字符串表示不使用代理", "", false},
		{"http 协议通过", "http://127.0.0.1:8080", false},
		{"socks5 协议通过", "socks5://127.0.0.1:1080", false},
		{"非法协议被拒绝", "ftp://127.0.0.1:21", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := defaultOptions()
			WithProxy(tt.proxy)(o)
			err := o.validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "WithProxy 仅支持")
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestValidateBreakerAndSizeLimit 校验熔断阈值与请求体上限的正数约束
func TestValidateBreakerAndSizeLimit(t *testing.T) {
	t.Run("熔断阈值零或负数被拒绝", func(t *testing.T) {
		for _, th := range []int{0, -1} {
			o := defaultOptions()
			WithCircuitBreaker(th)(o)
			err := o.validate()
			require.Error(t, err, "threshold=%d 应被拒绝", th)
			assert.Contains(t, err.Error(), "WithCircuitBreaker 的失败阈值必须为正数")
		}
	})

	t.Run("请求体上限零或负数被拒绝", func(t *testing.T) {
		for _, limit := range []int64{0, -100} {
			o := defaultOptions()
			WithRequestSizeLimit(limit)(o)
			err := o.validate()
			require.Error(t, err, "limit=%d 应被拒绝", limit)
			assert.Contains(t, err.Error(), "WithRequestSizeLimit 的上限必须为正数")
		}
	})

	t.Run("合法正数通过", func(t *testing.T) {
		o := defaultOptions()
		WithCircuitBreaker(5)(o)
		WithRequestSizeLimit(1024)(o)
		require.NoError(t, o.validate())
	})
}

// TestNewNilOptionIgnored 验证 nil Option 被跳过而非 panic（与 nacoscli/goredis 一致）
func TestNewNilOptionIgnored(t *testing.T) {
	client, err := New(nil, WithTimeout(3*time.Second), nil)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	assert.Equal(t, 3*time.Second, client.config.timeout)
}

// TestNewInvalidOptionsReturnError 验证非法配置在 New 构造期返回错误且不产生半成品客户端
func TestNewInvalidOptionsReturnError(t *testing.T) {
	client, err := New(WithTimeout(-1))
	assert.Nil(t, client)
	require.Error(t, err)
}

// TestNewEmptyBaseURLAllowed 验证 WithBaseURL("") 合法：等价于不设置基础 URL，
// 客户端可构造且必须由调用方传入完整地址发起请求（与 WithProxy 空串语义一致）
func TestNewEmptyBaseURLAllowed(t *testing.T) {
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	client := mustNew(t, WithBaseURL(""))
	_, err := client.Request(t.Context()).Get(ts.URL + "/full")
	require.NoError(t, err, "空 BaseURL 下传完整地址应可正常请求")
}

// TestNewCertificateErrorsReturnError 验证证书文件缺失在构造期返回错误（原实现打 stderr 后静默继续）
func TestNewCertificateErrorsReturnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such.pem")

	t.Run("根证书文件不存在", func(t *testing.T) {
		client, err := New(WithRootCA(missing))
		assert.Nil(t, client)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "读取根证书文件")
	})

	t.Run("根证书内容不是 PEM", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "bad.pem")
		require.NoError(t, os.WriteFile(bad, []byte("not a pem"), 0o600))
		client, err := New(WithRootCA(bad))
		assert.Nil(t, client)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "没有可解析的 PEM 证书")
	})

	t.Run("客户端证书与私钥不存在", func(t *testing.T) {
		client, err := New(WithClientCert(missing, missing))
		assert.Nil(t, client)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "加载客户端证书")
	})
}

// TestWithTracerProviderInjectsSpans 验证注入的 TracerProvider 真实生效（接口注入能力）
func TestWithTracerProviderInjectsSpans(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	client := mustNew(t, WithBaseURL(ts.URL), WithTracerProvider(tp))
	_, err := client.Request(t.Context()).Get("/traced")
	require.NoError(t, err)

	require.Len(t, sr.Ended(), 1, "注入的 provider 应产出 Span")
	assert.Equal(t, "GET /traced", sr.Ended()[0].Name())
}

// TestWithTracerProviderTypedNilFallsBack 验证 typed nil 回退全局 provider 而非 panic
func TestWithTracerProviderTypedNilFallsBack(t *testing.T) {
	var typedNil *sdktrace.TracerProvider
	o := defaultOptions()
	WithTracerProvider(typedNil)(o)
	assert.False(t, isUsableTracerProvider(o.tracerProvider), "typed nil 应被判定为不可用")

	o = defaultOptions()
	WithTracerProvider(nil)(o)
	assert.False(t, isUsableTracerProvider(o.tracerProvider))

	o = defaultOptions()
	real := sdktrace.NewTracerProvider()
	WithTracerProvider(real)(o)
	assert.True(t, isUsableTracerProvider(o.tracerProvider))
	require.NoError(t, real.Shutdown(t.Context()))
}

// TestNewDefaultConfig 验证未传 Option 时的默认配置与包级常量一致
func TestNewDefaultConfig(t *testing.T) {
	client, err := New()
	require.NoError(t, err)
	t.Cleanup(client.Close)

	assert.Equal(t, DefaultTimeout, client.config.timeout)
	assert.Nil(t, client.breaker, "未启用熔断时 breaker 为 nil")
	assert.EqualValues(t, 0, client.sizeLimit, "未设置上限时 sizeLimit 为 0")
	assert.NotNil(t, client.tracer)
}

// TestWithSSRFProtectionWiresGuards 验证 SSRF 选项在构造期装配到 transport 拨号链
func TestWithSSRFProtectionWiresGuards(t *testing.T) {
	client := mustNew(t, WithSSRFProtection())
	require.NotNil(t, client.transport)

	// 拨号内网地址应被拦截（装配成功即证明 DialContext 包装已生效）
	_, err := client.transport.DialContext(t.Context(), "tcp", "127.0.0.1:80")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSRFBlocked)
	assert.False(t, errors.Is(err, os.ErrNotExist), "应是 SSRF 拦截而非其他错误")
}
