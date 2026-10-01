package gosm3

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// FuzzHashInvariants 守护不变量：任意字节输入下——
//  1. 摘要恒为 32 字节、Hex 恒为 64 位合法编码；
//  2. Hash(x) 与 HashString(string(x)) 逐字节相等（两入口等价）；
//  3. 同一输入两次计算结果一致（确定性）；
//  4. 全程不 panic。
func FuzzHashInvariants(f *testing.F) {
	f.Add([]byte(""))                             // 空输入（Padding 最小块）
	f.Add([]byte("abc"))                          // GB/T 32905—2016 A.1 向量输入
	f.Add(bytes.Repeat([]byte("abcd"), 16))       // A.2 向量输入（64 字节，恰满块）
	f.Add(bytes.Repeat([]byte{0x00, 0xff}, 1024)) // 2KiB 二进制
	s := NewSM3()
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			t.Skip("限定输入不超过 64KiB")
		}
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
		// 两入口等价：HashString(string(x)) == Hash(x)
		viaString, err := s.HashString(string(data)).ToBytes()
		if err != nil {
			t.Fatalf("HashString 失败: %v", err)
		}
		if !bytes.Equal(viaString, digest) {
			t.Fatalf("Hash 与 HashString 不等价: %x vs %x", viaString, digest)
		}
		// 确定性：同一输入两次计算必须一致
		if again := mustHash(t, data); !bytes.Equal(again, digest) {
			t.Fatalf("同一输入两次摘要不一致: %x vs %x", again, digest)
		}
	})
}
