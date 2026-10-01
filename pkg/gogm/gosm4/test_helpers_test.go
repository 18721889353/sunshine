package gosm4

import "testing"

// testKey16 SM4 固定测试密钥（16 字节），保证用例输出可复现。
var testKey16 = []byte("1234567890123456")

// testIV16 SM4 固定测试 IV（16 字节），与 testKey16 配对使用。
var testIV16 = []byte("1234567890123456")

// newTestSM4 构造关闭 HTML 转义的 SM4 实例，保证明文字节与密文字节严格一一对应，
// 排除 unescapeHTML 对测试输入的干扰（HTML 行为由 TestUnescapeHTMLOption 单独覆盖）。
func newTestSM4(tb testing.TB) *SM4 {
	tb.Helper()
	return NewSM4(WithUnescapeHTML(false))
}
