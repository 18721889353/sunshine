package nacoscli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"github.com/18721889353/sunshine/pkg/logger"
)

// 本文件是 nacoscli 的性能基线（Benchmark）。
//
// 运行方式：
//
//	go test -bench=. -benchmem -run='^$' ./pkg/nacoscli/
//
// 口径说明：所有用例都使用 mockConfigClient，不产生网络流量，因此测得的是**本包自身开销**
// （context 检查 + goroutine/select + Span 创建 + 参数校验 + Option 归并 + 内容拷贝），
// 不包含 Nacos 服务端 RTT 与真实导出器的上报开销；用于回归「代码改动是否让热路径变慢」，
// 而不是给出绝对性能数字。

// benchmarkParams 返回一份通过校验的最小查询参数，避免各用例重复构造。
func benchmarkParams() *Params {
	return &Params{Group: "DEFAULT_GROUP", DataID: "application.yaml", Format: "yaml"}
}

// BenchmarkClientGetConfig 测量 GetConfig 成功路径的固定开销，并按配置内容大小分层。
// 关注点：goroutine + channel 交接与 []byte(r.data) 拷贝的成本随内容大小如何变化。
func BenchmarkClientGetConfig(b *testing.B) {
	sizes := []struct {
		name  string
		bytes int
	}{
		{"128B", 128},
		{"1KB", 1024},
		{"64KB", 64 * 1024},
	}

	for _, s := range sizes {
		b.Run(s.name, func(b *testing.B) {
			content := strings.Repeat("a", s.bytes)
			client := &Client{configClient: &mockConfigClient{
				getConfigFn: func(_ vo.ConfigParam) (string, error) {
					return content, nil
				},
			}}
			params := benchmarkParams()
			ctx := context.Background()

			b.ReportAllocs()
			b.SetBytes(int64(s.bytes))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				format, data, err := client.GetConfig(ctx, params)
				if err != nil {
					b.Fatalf("GetConfig 不应返回错误: %v", err)
				}
				if format != "yaml" || len(data) != s.bytes {
					b.Fatalf("返回内容与预期不符: format=%q len=%d", format, len(data))
				}
			}
		})
	}
}

// BenchmarkClientGetConfigWithRequestID 对比 ctx 中携带 request_id 时的开销增量。
// 该路径每次调用会额外设置一个 Span 属性，是线上最常见的形态。
func BenchmarkClientGetConfigWithRequestID(b *testing.B) {
	content := strings.Repeat("a", 1024)
	client := &Client{configClient: &mockConfigClient{
		getConfigFn: func(_ vo.ConfigParam) (string, error) {
			return content, nil
		},
	}}
	ctx := context.WithValue(context.Background(), logger.ContextKeyRequestID, "req-benchmark-id-0123456789")
	params := benchmarkParams()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := client.GetConfig(ctx, params); err != nil {
			b.Fatalf("GetConfig 不应返回错误: %v", err)
		}
	}
}

// BenchmarkValid 测量参数校验与 Format 归一化开销，覆盖成功与错误两条路径。
// 错误路径构造了 fmt.Errorf，成本显著高于成功路径，高频调用方应提前校验而非每轮重试。
func BenchmarkValid(b *testing.B) {
	cases := []struct {
		name   string
		format string
	}{
		{"yaml", "yaml"},
		{"yml归一化", "yml"},
		{"JSON大小写混合", "YaML"},
		{"不支持的类型", "xml"},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			params := &Params{Group: "DEFAULT_GROUP", DataID: "application.yaml", Format: c.format}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = params.valid()
			}
		})
	}
}

// BenchmarkApplyOptions 测量 Option 归并开销，含 nil Option 防御分支。
// GetConfig/NewListenClient 每次调用都会走一遍 apply，是热路径上的固定成本。
func BenchmarkApplyOptions(b *testing.B) {
	opts := []Option{
		WithIPAddr("127.0.0.1"),
		WithPort(8848),
		WithScheme("http"),
		WithContextPath("/nacos"),
		WithNamespaceID("public"),
		WithTimeoutMs(5000),
		WithAuth("nacos", "nacos"),
		WithGetTimeout(30 * time.Second),
		nil, // 动态拼接选项时的合法输入，apply 需跳过
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		o := defaultOptions()
		o.apply(opts...)
		if o.ipAddr != "127.0.0.1" || o.timeoutMs != 5000 {
			b.Fatalf("Option 未生效: %+v", o)
		}
	}
}

// BenchmarkBuildConfigs 测量从 options 构建 SDK 配置的两条路径。
// 「预设完整配置」直接返回指针，应明显快于「按单字段组装」。
func BenchmarkBuildConfigs(b *testing.B) {
	preset := &options{
		clientConfig:  &constant.ClientConfig{NamespaceId: "public", TimeoutMs: 5000},
		serverConfigs: []constant.ServerConfig{{IpAddr: "127.0.0.1", Port: 8848}},
	}
	assembled := &options{
		ipAddr: "127.0.0.1", port: 8848, scheme: "http", contextPath: "/nacos",
		namespaceID: "public", timeoutMs: 5000, username: "nacos", password: "nacos",
	}

	b.Run("预设完整配置", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, servers := buildConfigs(preset); len(servers) != 1 {
				b.Fatalf("ServerConfigs 构建异常: %+v", servers)
			}
		}
	})

	b.Run("按单字段组装", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			clientConfig, servers := buildConfigs(assembled)
			if clientConfig == nil || len(servers) != 1 {
				b.Fatal("配置构建结果异常")
			}
		}
	})
}

// BenchmarkRequestIDAttr 测量从 context 提取 request_id 的开销（命中与未命中）。
func BenchmarkRequestIDAttr(b *testing.B) {
	withID := context.WithValue(context.Background(), logger.ContextKeyRequestID, "req-attr-0123456789")

	b.Run("命中", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if attr := requestIDAttr(withID); attr.Key == "" {
				b.Fatal("携带 request_id 时应返回属性")
			}
		}
	})

	b.Run("未命中", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if attr := requestIDAttr(context.Background()); attr.Key != "" {
				b.Fatal("未携带 request_id 时应返回零值属性")
			}
		}
	})
}

// BenchmarkSafeAttrText 测量 Span 属性文本归一化的开销。
// 它在每次 GetConfig 上对 DataID/Group 各调一次，因此必须保证「合法输入」走零分配快路径。
func BenchmarkSafeAttrText(b *testing.B) {
	cases := []struct {
		name  string
		input string
	}{
		{"合法ASCII", "application.yaml"},
		{"合法中文", "配置中心.yaml"},
		{"含非法字节", "id\xff\xfedata"},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := safeAttrText(c.input); got == "" {
					b.Fatal("归一化结果不应为空")
				}
			}
		})
	}
}
