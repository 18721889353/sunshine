package tracer

import (
	"context"
	"math"
	"strings"
	"testing"
)

// 本文件守护包内纯函数的不变量（而非具体输出值），种子语料随常规 go test 运行，
// 挖掘深层输入需显式执行：go test -fuzz=FuzzXxx -fuzztime=30s ./pkg/tracer/

// FuzzClampRate 验证 clampRate 的两条不变量：
// 1. 输出恒落在 [0,1] 区间（含 NaN/±Inf 等特殊浮点输入）；
// 2. 幂等性：clampRate(clampRate(x)) == clampRate(x)。
func FuzzClampRate(f *testing.F) {
	f.Add(uint64(0))
	f.Add(math.Float64bits(0.5))
	f.Add(math.Float64bits(-1))
	f.Add(math.Float64bits(2))
	f.Add(math.Float64bits(math.NaN()))
	f.Add(math.Float64bits(math.Inf(1)))
	f.Add(math.Float64bits(math.Inf(-1)))

	f.Fuzz(func(t *testing.T, bits uint64) {
		x := math.Float64frombits(bits)
		got := clampRate(x)

		// 不变量 1：输出恒在 [0,1]（用 IsNaN + 边界判断，避免 NaN 从比较式中漏网）
		if math.IsNaN(got) || got < 0 || got > 1 {
			t.Fatalf("clampRate(%v) = %v，超出 [0,1]", x, got)
		}
		// 不变量 2：幂等
		if again := clampRate(got); again != got {
			t.Fatalf("clampRate 不幂等：clampRate(%v) = %v，再次钳位得 %v", x, got, again)
		}
	})
}

// FuzzIsHTTPEndpoint 验证 isHTTPEndpoint 的不变量：
// 1. 任意输入不 panic；
// 2. 判定为 HTTP 端点时，输入必然以 http:// 或 https:// 开头。
func FuzzIsHTTPEndpoint(f *testing.F) {
	f.Add("localhost:4317")
	f.Add("http://host:4318/v1/traces")
	f.Add("https://h/x")
	f.Add("://")
	f.Add("%zz")

	f.Fuzz(func(t *testing.T, endpoint string) {
		if isHTTPEndpoint(endpoint) &&
			!strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			t.Fatalf("isHTTPEndpoint(%q) 判定为 HTTP，但输入并非 http(s) 前缀", endpoint)
		}
	})
}

// FuzzNewSpanNeverPanics 验证 NewSpan 对任意标签键值与任意标签类型均不 panic
// （不支持的类型按约定降级为字符串存储）。
func FuzzNewSpanNeverPanics(f *testing.F) {
	f.Add("key", "value", uint8(0))
	f.Add("k", "", uint8(3))
	f.Add("k", "1.5", uint8(6))

	f.Fuzz(func(t *testing.T, key, value string, typeIdx uint8) {
		var v interface{}
		switch typeIdx % 5 {
		case 0:
			v = value
		case 1:
			v = []string{value}
		case 2:
			v = len(value)
		case 3:
			v = struct{ S string }{S: value} // 不支持的类型，走字符串降级
		case 4:
			v = nil
		}

		_, span := NewSpan(context.Background(), "fuzzSpan", map[string]interface{}{key: v})
		span.End()
	})
}
