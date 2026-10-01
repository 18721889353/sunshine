package gosm4

import (
	"bytes"
	"testing"
)

// benchPayload1K 基准载荷：1024 字节（64 × 16 字节块），模拟单次凭据/报文体量。
var benchPayload1K = bytes.Repeat([]byte("Nacos@SM4!Key#16"), 64)

// 基准口径：固定 1KiB 载荷、固定 key/iv、关闭 HTML 转义，只测算法本身，
// 不含任何网络/磁盘 RTT；对比不同机器或版本时必须固定 -benchtime=200000x。
// 读数见 README「基准与实测」章节。

// BenchmarkSM4CBCEncrypt 测量 1KiB 明文的 CBC 加密吞吐与分配。
func BenchmarkSM4CBCEncrypt(b *testing.B) {
	s := NewSM4(WithUnescapeHTML(false))
	b.ReportAllocs()
	b.SetBytes(int64(len(benchPayload1K)))
	for i := 0; i < b.N; i++ {
		if _, err := s.EncryptCBC(benchPayload1K, testKey16, testIV16).ToBytes(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSM4CBCDecrypt 测量 1KiB 密文的 CBC 解密吞吐与分配。
func BenchmarkSM4CBCDecrypt(b *testing.B) {
	s := NewSM4(WithUnescapeHTML(false))
	ciphertext, err := s.EncryptCBC(benchPayload1K, testKey16, testIV16).ToBytes()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(benchPayload1K)))
	for i := 0; i < b.N; i++ {
		if _, err := s.DecryptCBC(ciphertext, testKey16, testIV16).ToBytes(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSM4ECBEncrypt 测量 1KiB 明文的 ECB 加密吞吐与分配。
func BenchmarkSM4ECBEncrypt(b *testing.B) {
	s := NewSM4(WithUnescapeHTML(false))
	b.ReportAllocs()
	b.SetBytes(int64(len(benchPayload1K)))
	for i := 0; i < b.N; i++ {
		if _, err := s.EncryptECB(benchPayload1K, testKey16).ToBytes(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSM4ECBDecrypt 测量 1KiB 密文的 ECB 解密吞吐与分配。
func BenchmarkSM4ECBDecrypt(b *testing.B) {
	s := NewSM4(WithUnescapeHTML(false))
	ciphertext, err := s.EncryptECB(benchPayload1K, testKey16).ToBytes()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(benchPayload1K)))
	for i := 0; i < b.N; i++ {
		if _, err := s.DecryptECB(ciphertext, testKey16).ToBytes(); err != nil {
			b.Fatal(err)
		}
	}
}
