package gosm3

import "testing"

// mustHash 计算并返回 32 字节 SM3 摘要，失败即终止当前用例。
// 供单元测试与 fuzz 共用，吸收 ToBytes 的错误样板，使断言聚焦不变量本身。
func mustHash(tb testing.TB, input []byte) []byte {
	tb.Helper()
	digest, err := NewSM3().Hash(input).ToBytes()
	if err != nil {
		tb.Fatalf("计算摘要失败: %v", err)
	}
	return digest
}
