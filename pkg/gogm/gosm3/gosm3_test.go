package gosm3

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// sm3VectorABC SM3 标准测试向量 1：输入 "abc"（GB/T 32905—2016 A.1 节，
// 亦见 GM/T 0004—2012 标准文本；GmSSL/OpenSSL 实现同值）。
const sm3VectorABC = "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0"

// sm3VectorABCD16 SM3 标准测试向量 2：输入64 字节 "abcdabcd...abcd"（GB/T 32905—2016 A.2 节）。
const sm3VectorABCD16 = "debe9ff92275b8a138604889c18e5a4d6fdb70e5387e5765293dcba39c0c5732"

// TestHashKnownVectors 验证 Hash/HashString 对 GB/T 32905—2016 官方向量的计算结果。
func TestHashKnownVectors(t *testing.T) {
	s := NewSM3()

	t.Run("abc标准向量", func(t *testing.T) {
		got, err := s.HashString("abc").ToHex()
		if err != nil {
			t.Fatalf("计算哈希失败: %v", err)
		}
		if got != sm3VectorABC {
			t.Fatalf("得到 %s, 期望 %s", got, sm3VectorABC)
		}
	})

	t.Run("64字节标准向量", func(t *testing.T) {
		input := bytes.Repeat([]byte("abcd"), 16)
		got, err := s.Hash(input).ToHex()
		if err != nil {
			t.Fatalf("计算哈希失败: %v", err)
		}
		if got != sm3VectorABCD16 {
			t.Fatalf("得到 %s, 期望 %s", got, sm3VectorABCD16)
		}
	})
}

// TestHashInvariants 验证摘要计算的基本不变量：输出恒为 32 字节 / 64 位 Hex、
// HashString 与 Hash 等价、不同输入摘要不同。
func TestHashInvariants(t *testing.T) {
	s := NewSM3()

	t.Run("输出恒为32字节", func(t *testing.T) {
		for _, data := range [][]byte{nil, []byte("a"), bytes.Repeat([]byte("x"), 1000)} {
			digest := mustHash(t, data)
			if len(digest) != 32 {
				t.Fatalf("摘要长度应为 32 字节, 实际 %d", len(digest))
			}
			hexStr, err := s.Hash(data).ToHex()
			if err != nil {
				t.Fatalf("转 Hex 失败: %v", err)
			}
			if len(hexStr) != 64 {
				t.Fatalf("Hex 摘要应为 64 位, 实际 %d", len(hexStr))
			}
			if _, err := hex.DecodeString(hexStr); err != nil {
				t.Fatalf("Hex 摘要非法: %v", err)
			}
		}
	})

	t.Run("HashString与Hash等价", func(t *testing.T) {
		const data = "equivalence check"
		fromString, err := s.HashString(data).ToBytes()
		if err != nil {
			t.Fatalf("HashString 失败: %v", err)
		}
		fromBytes := mustHash(t, []byte(data))
		if !bytes.Equal(fromString, fromBytes) {
			t.Fatalf("同一输入的两种入口摘要不一致")
		}
	})

	t.Run("不同输入摘要不同", func(t *testing.T) {
		a, err := s.HashString("input-a").ToBytes()
		if err != nil {
			t.Fatalf("计算失败: %v", err)
		}
		b, err := s.HashString("input-b").ToBytes()
		if err != nil {
			t.Fatalf("计算失败: %v", err)
		}
		if bytes.Equal(a, b) {
			t.Fatalf("不同输入的摘要不应相同")
		}
	})
}
