package jwt

import (
	"testing"
)

// 本文件是包的性能基线：全部基准只依赖包内配置与纯 CPU 计算（HMAC 签名 + JSON 序列化），
// 不含任何网络 RTT，定位是「相对回归基线」而非生产延迟。
// 读数与口径说明见 README「性能基线与模糊测试」。
//
// 并发安全说明：各 Benchmark 通过 newTestManager 创建独立 Manager 实例，配置互不共享，
// 不存在全局可变状态，基准之间（以及未来改为并发执行时）互不污染。

// BenchmarkGenerateToken 基准标准 token 签发开销（含 1 个自定义字段）。
func BenchmarkGenerateToken(b *testing.B) {
	mgr := newTestManager(b)
	fields := map[string]any{"role": "admin"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.GenerateToken("10001", "sunshine", fields); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseToken 基准标准 token 解析与校验开销（签名验证 + claims 反序列化）。
func BenchmarkParseToken(b *testing.B) {
	mgr := newTestManager(b)
	token, err := mgr.GenerateToken("10001", "sunshine", map[string]any{"role": "admin"})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.ParseToken(token); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGenerateCustomToken 基准自定义字段 token 签发开销（5 个字段）。
func BenchmarkGenerateCustomToken(b *testing.B) {
	mgr := newTestManager(b)
	fields := KV{"name": "sunshine", "age": 10, "id": 20, "vip": true, "score": 9.5}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.GenerateCustomToken(fields); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseCustomToken 基准自定义字段 token 解析与校验开销。
func BenchmarkParseCustomToken(b *testing.B) {
	mgr := newTestManager(b)
	token, err := mgr.GenerateCustomToken(KV{"name": "sunshine", "age": 10, "id": 20, "vip": true, "score": 9.5})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.ParseCustomToken(token); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRefreshToken 基准标准 token 刷新开销（解析 + 重新签发的组合路径）。
func BenchmarkRefreshToken(b *testing.B) {
	mgr := newTestManager(b)
	token, err := mgr.GenerateToken("10001", "sunshine", nil)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.RefreshToken(token); err != nil {
			b.Fatal(err)
		}
	}
}
