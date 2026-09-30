package gohttp

import (
	"context"
	"testing"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
)

// 本文件是 gohttp 的性能基线（Benchmark）。
//
// 运行方式：
//
//	go test -bench=. -benchmem -run='^$' ./pkg/gohttp/
//
// 口径说明：所有用例都是**纯本包函数**（校验/脱敏/估算/熔断状态机/构造），
// 不发起任何网络请求、不含回环 RTT，测得的是「代码改动是否让热路径变慢」的回归基线，
// 不是绝对吞吐数字。含真实 RTT 的端到端读数见 integration_test.go 与 README 实测章节。

// BenchmarkValidateURL 测量 URL 字面量校验开销（成功与拦截两条路径）。
// NewRequestWithValidation 每次调用都会走它，属于请求热路径上的固定成本。
func BenchmarkValidateURL(b *testing.B) {
	cases := []struct {
		name   string
		rawURL string
	}{
		{"公网域名通过", "https://api.example.com/v1/users?page=1"},
		{"内网字面量拦截", "http://10.0.0.1/admin"},
		{"非法协议拒绝", "ftp://example.com/file"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = ValidateURL(c.rawURL)
			}
		})
	}
}

// BenchmarkRedactURL 测量错误文本脱敏开销（正则替换是每条 transport 错误的必经路径）。
// 无敏感信息时两次正则扫描均不命中，应保持低分配。
func BenchmarkRedactURL(b *testing.B) {
	cases := []struct {
		name  string
		input string
	}{
		{"无敏感信息", "Get \"http://api.example.com/v1/items?page=2\": dial tcp: i/o timeout"},
		{"敏感参数命中", "Get \"http://u:p@10.0.0.1:8080/x?token=SUPER_SECRET&client_secret=abc\": connection refused"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = redactURL(c.input)
			}
		})
	}
}

// BenchmarkBodySize 测量请求体长度估算开销（checkSizeLimit 的核心，每个带 body 的请求调用一次）。
func BenchmarkBodySize(b *testing.B) {
	payload := make([]byte, 32*1024)

	b.Run("[]byte已知长度", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			size, known := bodySize(payload)
			if !known || size != int64(len(payload)) {
				b.Fatalf("估算错误: size=%d known=%v", size, known)
			}
		}
	})

	b.Run("未知类型不预知", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, known := bodySize(nopCloserBody{}); known {
				b.Fatal("未知类型应判定为长度不可知")
			}
		}
	})
}

// nopCloserBody 模拟无法预知长度的流式请求体类型
type nopCloserBody struct{}

// BenchmarkCircuitBreakerAllow 测量熔断放行路径开销（每个请求的 onBeforeRequest 必经）。
// 关闭状态下只有一次加锁与状态判断，应近似零分配。
func BenchmarkCircuitBreakerAllow(b *testing.B) {
	breaker := newCircuitBreaker(5, time.Minute)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := breaker.allow(); err != nil {
			b.Fatalf("关闭状态应放行: %v", err)
		}
	}
}

// BenchmarkRequestIDAttr 测量从 context 提取 request_id 写入 Span 属性的开销
// （命中与未命中两条路径，每个请求调用一次）。
func BenchmarkRequestIDAttr(b *testing.B) {
	withID := context.WithValue(context.Background(), logger.ContextKeyRequestID, "req-benchmark-0123456789")

	b.Run("命中", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if attr := requestIDAttr(withID); attr.Value.AsString() == "" {
				b.Fatal("携带 request_id 时应返回属性")
			}
		}
	})

	b.Run("未命中", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = requestIDAttr(context.Background())
		}
	})
}

// BenchmarkOptionsApplyAndValidate 测量「Option 归并 + 构造期校验」开销。
// New 的第一步，配置非法时在这里快速失败；不含 transport/resty 构建与证书 IO。
func BenchmarkOptionsApplyAndValidate(b *testing.B) {
	opts := []Option{
		WithBaseURL("https://api.example.com"),
		WithTimeout(5 * time.Second),
		WithRetry(3, time.Second, 5*time.Second),
		WithCircuitBreaker(10),
		WithRequestSizeLimit(1 << 20),
		nil, // 动态拼接选项时的合法输入，apply 需跳过
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		o := defaultOptions()
		o.apply(opts...)
		if err := o.validate(); err != nil {
			b.Fatalf("合法配置不应校验失败: %v", err)
		}
	}
}

// BenchmarkNewClient 测量完整构造开销（含 resty 客户端与 transport 组装，无网络 IO）。
// 构造通常在进程启动期执行一次，读数用于观察配置装配是否引入意外分配。
func BenchmarkNewClient(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := New(WithBaseURL("https://api.example.com"), WithRetry(0, 0, 0))
		if err != nil {
			b.Fatalf("New 不应失败: %v", err)
		}
		c.Close()
	}
}
