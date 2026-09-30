package gohttp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件对应 security.go 的一对一测试（SSRF 防护 / 熔断器 / 错误脱敏 / 请求体上限）。

// ========== ValidateURL ==========

// TestValidateURL 校验 URL 字面量白名单：协议、主机名与内网 IP 字面量拦截
func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantIs  error // 按 errors.Is 断言的哨兵错误
		wantErr bool
	}{
		{"空 URL 被拒绝", "", nil, true},
		{"格式非法被拒绝", "://bad", nil, true},
		{"非 http 协议被拒绝", "ftp://example.com/api", nil, true},
		{"缺少主机名被拒绝", "http:///path", nil, true},
		{"内网 IP 字面量被拦截", "http://10.0.0.1/api", ErrSSRFBlocked, true},
		{"回环 IP 字面量被拦截", "http://127.0.0.1:8080/api", ErrSSRFBlocked, true},
		{"公网 IP 字面量通过", "http://93.184.216.34/api", nil, false},
		{"域名通过（字面量校验不查 DNS）", "https://example.com/v1", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.rawURL)
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

// TestIsPrivateIP 验证私有/特殊地址段判定（含云元数据地址与 IPv6 回环）
func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"回环地址", "127.0.0.1", true},
		{"A 类私有", "10.1.2.3", true},
		{"B 类私有", "172.16.0.1", true},
		{"C 类私有", "192.168.1.1", true},
		{"运营商 NAT", "100.64.0.1", true},
		{"链路本地/云元数据", "169.254.169.254", true},
		{"IPv6 回环", "::1", true},
		{"公网 DNS", "8.8.8.8", false},
		{"公网地址", "93.184.216.34", false},
		{"IPv6 公网", "2001:4860:4860::8888", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isPrivateIP(net.ParseIP(tt.ip)))
		})
	}
}

// ========== SSRF 拨号层防护 ==========

// withFakeSSRFResolver 替换域名解析入口并在测试结束后恢复（注入解析结果，避免网络依赖）
func withFakeSSRFResolver(t *testing.T, lookup func(ctx context.Context, host string) ([]net.IPAddr, error)) {
	t.Helper()
	prev := ssrfLookup
	ssrfLookup = lookup
	t.Cleanup(func() { ssrfLookup = prev })
}

// TestApplySSRFProtectionBlocksDomain 验证域名解析到内网地址时拨号被拦截。
// 回归背景：原实现只在 host 本身是 IP 字面量时才校验，攻击者控制的 DNS 记录
// 指向 127.0.0.1 可完全绕过防护。
func TestApplySSRFProtectionBlocksDomain(t *testing.T) {
	withFakeSSRFResolver(t, func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
	})

	var dialed atomic.Int32
	tr := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed.Add(1)
			return nil, errors.New("不应发生实际拨号")
		},
	}
	applySSRFProtection(tr)

	_, err := tr.DialContext(context.Background(), "tcp", "evil.example.com:80")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSSRFBlocked)
	assert.Zero(t, dialed.Load(), "内网目标必须在拨号前被拦截")
}

// TestApplySSRFProtectionDialsValidatedIP 验证域名解析到公网地址后**直拨已校验的 IP**，
// 关闭「校验完成到实际拨号之间」的 DNS 重绑定（rebinding）窗口
func TestApplySSRFProtectionDialsValidatedIP(t *testing.T) {
	withFakeSSRFResolver(t, func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	})

	var gotAddr atomic.Value
	tr := &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			gotAddr.Store(addr)
			local, remote := net.Pipe()
			_ = remote.Close()
			return local, nil
		},
	}
	applySSRFProtection(tr)

	conn, err := tr.DialContext(context.Background(), "tcp", "example.com:443")
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = conn.Close()

	assert.Equal(t, "93.184.216.34:443", gotAddr.Load(),
		"应直拨校验通过的 IP 而非回拨域名")
}

// TestApplySSRFProtectionResolverFailures 验证解析失败与空结果均被拒绝（fail-closed）
func TestApplySSRFProtectionResolverFailures(t *testing.T) {
	t.Run("解析失败被拒绝", func(t *testing.T) {
		withFakeSSRFResolver(t, func(context.Context, string) ([]net.IPAddr, error) {
			return nil, errors.New("dns unavailable")
		})
		tr := &http.Transport{}
		applySSRFProtection(tr)
		_, err := tr.DialContext(context.Background(), "tcp", "a.example.com:80")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "解析域名")
	})

	t.Run("解析结果为空被拒绝", func(t *testing.T) {
		withFakeSSRFResolver(t, func(context.Context, string) ([]net.IPAddr, error) {
			return nil, nil
		})
		tr := &http.Transport{}
		applySSRFProtection(tr)
		_, err := tr.DialContext(context.Background(), "tcp", "b.example.com:80")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "未解析到任何地址")
	})

	t.Run("混合结果中含内网地址即整体拦截", func(t *testing.T) {
		withFakeSSRFResolver(t, func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{
				{IP: net.ParseIP("93.184.216.34")},
				{IP: net.ParseIP("192.168.0.1")},
			}, nil
		})
		tr := &http.Transport{}
		applySSRFProtection(tr)
		_, err := tr.DialContext(context.Background(), "tcp", "mixed.example.com:80")
		require.ErrorIs(t, err, ErrSSRFBlocked)
	})
}

// ========== 熔断器状态机 ==========

// currentState 返回熔断器当前状态，供测试断言状态机迁移。
// 刻意定义在测试文件而非 security.go：它只有测试调用，放生产文件会命中
// golangci-lint 的 unused（本仓 run.tests: false，测试使用点不算引用）。
func (b *circuitBreaker) currentState() breakerState {
	if b == nil {
		return breakerClosed
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// TestCircuitBreakerOpensAfterThreshold 验证关闭状态下连续失败达到阈值后打开并拒绝请求
func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	b := newCircuitBreaker(3, time.Minute)

	for i := 0; i < 2; i++ {
		require.NoError(t, b.allow(), "未达阈值前应放行（第 %d 次）", i+1)
		require.Equal(t, breakerClosed, b.currentState())
		b.onFailure()
	}

	require.NoError(t, b.allow(), "第 3 次失败前仍放行")
	b.onFailure()
	require.Equal(t, breakerOpen, b.currentState(), "达到阈值应打开")

	err := b.allow()
	require.ErrorIs(t, err, ErrCircuitBreakerOpen)
	assert.Contains(t, err.Error(), "冷却")
}

// TestCircuitBreakerHalfOpenProbeLifecycle 验证完整状态机：
// 打开 → 冷却期满放行唯一探测 → 探测成功回关闭；再次打开 → 探测失败 → 重新打开。
// 冷却等待使用命名常量 shortTestCooldown（取值依据与不 flaky 说明见 test_helpers_test.go）
func TestCircuitBreakerHalfOpenProbeLifecycle(t *testing.T) {
	b := newCircuitBreaker(2, shortTestCooldown)

	// 阶段一：连续两次失败打开
	require.NoError(t, b.allow())
	b.onFailure()
	require.NoError(t, b.allow())
	b.onFailure()
	require.Equal(t, breakerOpen, b.currentState())

	// 冷却期内拒绝
	require.ErrorIs(t, b.allow(), ErrCircuitBreakerOpen)

	// 阶段二：冷却期满 → 半开，放行唯一探测
	time.Sleep(shortTestCooldown)
	require.NoError(t, b.allow(), "冷却期满应转入半开并放行探测")
	require.Equal(t, breakerHalfOpen, b.currentState())
	require.ErrorIs(t, b.allow(), ErrCircuitBreakerOpen, "半开状态下只允许一个在途探测")

	// 探测成功 → 关闭并清零计数
	b.onSuccess()
	require.Equal(t, breakerClosed, b.currentState())

	// 阶段三：再次累计失败打开，探测失败则重新打开
	require.NoError(t, b.allow())
	b.onFailure()
	require.NoError(t, b.allow())
	b.onFailure()
	require.Equal(t, breakerOpen, b.currentState())

	time.Sleep(shortTestCooldown)
	require.NoError(t, b.allow())
	b.onFailure()
	require.Equal(t, breakerOpen, b.currentState(), "半开探测失败应重新打开")
}

// TestCircuitBreakerNilSafe 验证未启用熔断时（nil receiver）全部方法安全放行
func TestCircuitBreakerNilSafe(t *testing.T) {
	var b *circuitBreaker
	require.NoError(t, b.allow())
	require.NotPanics(t, func() {
		b.onSuccess()
		b.onFailure()
	})
	assert.Equal(t, breakerClosed, b.currentState())
}

// TestCircuitBreakerConcurrentProbeSingleFlight 验证半开状态下并发探测只有一个被放行。
// 用 WaitGroup 等待全部 goroutine 结果（基线第三节：禁止裸 sleep 判定）
func TestCircuitBreakerConcurrentProbeSingleFlight(t *testing.T) {
	b := newCircuitBreaker(1, 0) // cooldown=0：打开后立即满足冷却条件
	b.onFailure()                // 阈值 1 → 立即打开

	const workers = 32
	var (
		wg       sync.WaitGroup
		allowed  atomic.Int32
		rejected atomic.Int32
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if err := b.allow(); err != nil {
				if errors.Is(err, ErrCircuitBreakerOpen) {
					rejected.Add(1)
				}
				return
			}
			allowed.Add(1)
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 1, allowed.Load(), "半开探测应只放行一个")
	assert.EqualValues(t, workers-1, rejected.Load())
}

// ========== 熔断计数口径（端到端） ==========

// TestClientBreakerRejectsRequestAfterFailure 验证 5xx 计为失败、达到阈值后
// 后续请求在本地被拒绝（不再触达下游）
func TestClientBreakerRejectsRequestAfterFailure(t *testing.T) {
	var hits atomic.Int32
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	client := mustNew(t,
		WithBaseURL(ts.URL),
		WithRetry(0, 0, 0),
		WithCircuitBreaker(1),
	)

	// 第一次请求：下游 500 → 计为失败 → 熔断打开
	_, err := client.Request(t.Context()).Get("/fail")
	require.Error(t, err)
	var httpErr *ErrorResponse
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusInternalServerError, httpErr.StatusCode)

	// 第二次请求：本地拒绝，不再触达下游
	_, err = client.Request(t.Context()).Get("/fail")
	require.ErrorIs(t, err, ErrCircuitBreakerOpen)
	assert.EqualValues(t, 1, hits.Load(), "熔断打开后不应再请求下游")
}

// TestClientBreakerTreats4xxAsSuccess 验证 4xx 计为成功（下游可达，故障在调用方）
func TestClientBreakerTreats4xxAsSuccess(t *testing.T) {
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	client := mustNew(t,
		WithBaseURL(ts.URL),
		WithRetry(0, 0, 0),
		WithCircuitBreaker(1), // 阈值 1：若 4xx 被计为失败，第二次请求必被拦截
	)

	for i := 0; i < 3; i++ {
		_, err := client.Request(t.Context()).Get("/missing")
		require.Error(t, err, "第 %d 次应返回 404 业务错误", i+1)
		var httpErr *ErrorResponse
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, http.StatusNotFound, httpErr.StatusCode)
	}
	assert.Equal(t, client.breaker.currentState(), breakerClosed, "4xx 不应累计熔断失败")
}

// ========== 请求体大小上限 ==========

// TestBodySizeKnownTypes 验证 bodySize 对各类请求体的长度估算与未知类型判定
func TestBodySizeKnownTypes(t *testing.T) {
	tests := []struct {
		name      string
		body      interface{}
		wantSize  int64
		wantKnown bool
	}{
		{"nil 体长度为零", nil, 0, true},
		{"字节切片", []byte("abc"), 3, true},
		{"字符串", "abcd", 4, true},
		{"bytes.Buffer", bytes.NewBufferString("xy"), 2, true},
		{"strings.Reader", strings.NewReader("abcd"), 4, true},
		{"未知 reader 不可预知", io.LimitReader(strings.NewReader("12345"), 5), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, known := bodySize(tt.body)
			assert.Equal(t, tt.wantKnown, known)
			if tt.wantKnown {
				assert.Equal(t, tt.wantSize, size)
			}
		})
	}
}

// TestClientSizeLimitRejectsBody 验证超限请求体在发送前被拒绝且不触达下游。
// 回归背景：原实现读 req.RawRequest.ContentLength，但 resty 的用户中间件先于内置
// createHTTPRequest 执行，RawRequest 恒为 nil，上限从未生效（死代码）
func TestClientSizeLimitRejectsBody(t *testing.T) {
	var hits atomic.Int32
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL), WithRequestSizeLimit(4))

	_, err := client.Request(t.Context()).
		SetBody([]byte("0123456789")).
		Post("/upload")
	require.ErrorIs(t, err, ErrRequestTooLarge)
	assert.Zero(t, hits.Load(), "超限请求不应发出")
}

// TestClientSizeLimitAllowsKnownSize 验证未超限的请求体正常放行
func TestClientSizeLimitAllowsKnownSize(t *testing.T) {
	var gotBody string
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL), WithRequestSizeLimit(1024))

	_, err := client.Request(t.Context()).
		SetBody([]byte("small")).
		Post("/upload")
	require.NoError(t, err)
	assert.Equal(t, "small", gotBody)
}

// TestClientSizeLimitAllowsStream 验证流式请求体（长度未知）不被拦截——
// 边界与 checkSizeLimit 注释一致：无法预知大小的类型不做限制
func TestClientSizeLimitAllowsStream(t *testing.T) {
	var hits atomic.Int32
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL), WithRequestSizeLimit(4))

	stream := io.LimitReader(strings.NewReader(strings.Repeat("x", 1024)), 1024)
	_, err := client.Request(t.Context()).SetBody(stream).Post("/stream")
	require.NoError(t, err, "长度未知的流式体不应被上限拦截")
	assert.EqualValues(t, 1, hits.Load())
}

// ========== 错误脱敏 ==========

// TestRedactURL 验证 userinfo 密码与敏感 query 参数被替换、普通参数保留
func TestRedactURL(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		notCon   string // 结果中不应出现的敏感明文
		wantSubs []string
	}{
		{
			"userinfo 密码脱敏",
			"Get http://user:pass@10.1.2.3:8080/path: dial tcp",
			"pass",
			[]string{"user:***@", "dial tcp"},
		},
		{
			"token 参数脱敏",
			"http://api.example.com/v1/users?token=SUPER_SECRET&limit=10",
			"SUPER_SECRET",
			[]string{"token=***", "limit=10"},
		},
		{
			"client_secret 与 password 同时脱敏",
			"http://api.example.com/oauth?client_secret=abc123&password=pwd456&name=tom",
			"abc123",
			[]string{"client_secret=***", "password=***", "name=tom"},
		},
		{
			"大小写不敏感",
			"http://api.example.com/x?ACCESS_TOKEN=TOK999",
			"TOK999",
			[]string{"ACCESS_TOKEN=***"},
		},
		{
			"key 属歧义参数刻意不脱敏",
			"http://api.example.com/x?key=pagevalue",
			"",
			[]string{"key=pagevalue"},
		},
		{"无敏感信息原样保留", "http://api.example.com/v1/items?page=2", "", []string{"page=2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactURL(tt.in)
			if tt.notCon != "" {
				assert.NotContains(t, got, tt.notCon, "敏感明文不应出现在结果中")
			}
			for _, sub := range tt.wantSubs {
				assert.Contains(t, got, sub)
			}
		})
	}
}

// TestRedactedErrorChain 验证脱敏错误对外不泄漏敏感文本，同时错误链可被
// errors.Is/As 穿透（调用方仍能识别底层错误类型）
func TestRedactedErrorChain(t *testing.T) {
	inner := errors.New(`Get "http://user:topsecret@127.0.0.1:1/x?token=TKN": dial tcp: refused`)
	wrapped := &redactedError{
		err: inner,
		msg: "gohttp: transport layer execution failed: " + redactURL(inner.Error()),
	}

	assert.NotContains(t, wrapped.Error(), "topsecret", "错误文本不得泄漏 userinfo 密码")
	assert.NotContains(t, wrapped.Error(), "TKN", "错误文本不得泄漏敏感 query")
	assert.Contains(t, wrapped.Error(), "***", "敏感值应替换为占位符")

	assert.ErrorIs(t, wrapped, inner, "错误链必须保持可穿透")
	assert.Equal(t, inner, errors.Unwrap(wrapped))
}
