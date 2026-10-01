package gosm2

import (
	"bytes"
	"testing"
)

// benchPayloadSM2 基准载荷：136 字节明文，模拟单次报文签名/加密的典型体量。
var benchPayloadSM2 = bytes.Repeat([]byte("sunshine-gogm-sm2"), 8)

// 基准口径：固定 136 字节载荷、关闭 HTML 转义，只测算法本身，不含网络/磁盘 RTT；
// 对比不同机器或版本时必须固定 -benchtime=200000x。读数见 README「基准与实测」章节。

// BenchmarkGenerateKeyPair 测量一次 SM2 密钥对生成（含 PEM/Hex 序列化）的耗时与分配。
func BenchmarkGenerateKeyPair(b *testing.B) {
	s := NewSM2()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.GenerateKeyPair(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncryptC1C3C2 测量 136 字节明文的 SM2 加密耗时与分配。
func BenchmarkEncryptC1C3C2(b *testing.B) {
	s := newTestSM2(b)
	_, _, publicKey := mustGenerateKeyPair(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(benchPayloadSM2)))
	for i := 0; i < b.N; i++ {
		if _, err := s.Encrypt(publicKey, benchPayloadSM2, C1C3C2).ToBytes(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSign 测量 136 字节数据的 SM2 签名耗时与分配。
func BenchmarkSign(b *testing.B) {
	s := newTestSM2(b)
	_, privateKey, _ := mustGenerateKeyPair(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(benchPayloadSM2)))
	for i := 0; i < b.N; i++ {
		if _, err := s.Sign(privateKey, benchPayloadSM2).ToBytes(); err != nil {
			b.Fatal(err)
		}
	}
}
