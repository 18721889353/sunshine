package gosm3

import (
	"bytes"
	"testing"
)

// benchPayload1K 基准载荷：1024 字节，测量跨多个 64 字节压缩块的哈希性能。
var benchPayload1K = bytes.Repeat([]byte("sunshine-gogm-sm3"), 61)[:1024] // 17×61=1107 ≥ 1024

// 基准口径：固定 1KiB 载荷，只测算法本身，不含网络/磁盘 RTT；
// 对比不同机器或版本时必须固定 -benchtime=200000x。读数见 README「基准与实测」章节。

// BenchmarkHash 测量 1KiB 字节切片的 SM3 摘要耗时与分配。
func BenchmarkHash(b *testing.B) {
	s := NewSM3()
	b.ReportAllocs()
	b.SetBytes(int64(len(benchPayload1K)))
	for i := 0; i < b.N; i++ {
		if _, err := s.Hash(benchPayload1K).ToBytes(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHashString 测量 1KiB 字符串的 SM3 摘要耗时与分配。
func BenchmarkHashString(b *testing.B) {
	s := NewSM3()
	payload := string(benchPayload1K)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for i := 0; i < b.N; i++ {
		if _, err := s.HashString(payload).ToHex(); err != nil {
			b.Fatal(err)
		}
	}
}
