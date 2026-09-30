package gohttp

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 本文件是 gohttp 的模糊测试（Fuzz test），目标是把「不变量」交给随机输入反复冲击：
// 脱敏不泄漏敏感值且幂等、URL 校验不 panic 且错误文本可安全写日志、
// 请求体长度估算不 panic 且判定一致。
//
// 运行方式：
//
//	# 只跑内置种子语料（默认随 go test 执行，耗时可忽略）
//	go test -run='^Fuzz' ./pkg/gohttp/
//	# 真正的模糊挖掘（不进 CI 常规流程，按需本地跑）
//	go test -fuzz=FuzzRedactURL   -fuzztime=30s ./pkg/gohttp/
//	go test -fuzz=FuzzValidateURL -fuzztime=30s ./pkg/gohttp/
//	go test -fuzz=FuzzBodySize    -fuzztime=30s ./pkg/gohttp/
//
// 失败语料会写入 testdata/fuzz/<TargetName>/，需要人工确认后提交或修正。

// redactSensitiveNames 参与脱敏的敏感参数名（与 security.go 的正则名单一致，
// 此处仅用于测试构造输入，改名单时两处同步）
var redactSensitiveNames = []string{
	"access_token", "refresh_token", "id_token", "token",
	"api_key", "apikey", "client_secret", "secret",
	"password", "passwd", "pwd", "authorization", "signature",
}

// FuzzRedactURL 验证错误脱敏的三条不变量：
//  1. 任意输入不 panic；
//  2. 幂等——脱敏结果再次脱敏必须保持不变（否则日志被重复包装时会变形）；
//  3. 构造的敏感参数对 `name=secret` 不再出现在结果中（值不含 & 与空白时，正则会完整吞掉它）。
//     断言参数对而非孤立的值：若只断言「值不出现」，当 fuzz 输入本身携带同名子串时会产生假阳性；
//     而参数对一旦存在就该被脱敏，无论它出现在输入的哪个位置。
func FuzzRedactURL(f *testing.F) {
	seeds := []string{
		"http://api.example.com/v1/items?page=2",
		"Get \"http://user:pass@10.0.0.1:8080/x?token=SECRET\": dial tcp: refused",
		"http://x/p?client_secret=abc&password=pwd&name=tom",
		"http://x/p?ACCESS_TOKEN=CAPS&key=normal",
		"无协议文本 token=inline 敏感值",
		"",
		"\x00\x01\x02 with control chars",
		strings.Repeat("a", 4096),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		got := redactURL(raw)

		// 不变量 2：幂等
		require.Equal(t, got, redactURL(got), "脱敏必须幂等")

		// 不变量 3：构造一个敏感参数值，若其不含正则终止字符，则必须被吞掉
		secret := "S3cr3tVal" + strings.Repeat("z", len(raw)%16)
		name := redactSensitiveNames[len(raw)%len(redactSensitiveNames)]
		input := raw + "?" + name + "=" + secret
		out := redactURL(input)
		require.NotContains(t, out, name+"="+secret,
			"敏感参数 %s 的值不得出现在脱敏结果中", name)
	})
}

// FuzzValidateURL 验证 URL 校验的不变量：
//  1. 任意输入不 panic；
//  2. 校验通过时必然满足「http/https 协议 + 非空主机名」（通过即代表可继续请求）；
//  3. 错误文本不含裸换行/回车——错误会进入日志与上层响应体，裸换行会造成日志注入。
func FuzzValidateURL(f *testing.F) {
	seeds := []string{
		"https://api.example.com/v1",
		"http://10.0.0.1/admin",
		"ftp://example.com/file",
		"",
		"://bad",
		"http:///path",
		"http://user:pass@host/x?token=abc",
		"http://[::1]/v6",
		"javascript:alert(1)",
		"HTTP://EXAMPLE.COM/UPPER",
		"带中文的URL",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		err := ValidateURL(raw)
		if err == nil {
			// 通过校验 ⇒ 协议白名单与主机名约束必须成立。
			// 必须重新 Parse 而不是对 raw 做前缀判断：url.Parse 会把 scheme 归一为小写
			// （实测 `HTTP://EXAMPLE.COM` 解析后 scheme="http"、host 原样），
			// 直接 HasPrefix(raw, "http://") 会把合法的大写输入误判为漏洞。
			u, parseErr := url.Parse(raw)
			require.NoError(t, parseErr, "校验通过的 URL 必须可解析: %q", raw)
			require.Contains(t, []string{"http", "https"}, u.Scheme, "协议必须在白名单内: %q", raw)
			require.NotEmpty(t, u.Hostname(), "主机名不得为空: %q", raw)
			return
		}
		require.NotContains(t, err.Error(), "\n", "错误消息不得含裸换行")
		require.NotContains(t, err.Error(), "\r", "错误消息不得含裸回车")
	})
}

// FuzzBodySize 验证请求体长度估算的不变量：
//  1. 任意输入不 panic；
//  2. 判定为「已知」时长度必须非负，且 []byte / string 必须给出精确长度。
//
// checkSizeLimit 依赖这两个性质决定是否放行，误判会放大成越界写或误杀正常请求。
func FuzzBodySize(f *testing.F) {
	f.Add([]byte(nil), "")
	f.Add([]byte("abc"), "abcd")
	f.Add([]byte(strings.Repeat("x", 1024)), strings.Repeat("y", 1024))
	f.Add([]byte{0x00, 0xff, 0xfe}, "带\n控制符")

	f.Fuzz(func(t *testing.T, bin []byte, txt string) {
		size, known := bodySize(bin)
		require.True(t, known, "[]byte 必须判定为长度已知")
		require.EqualValues(t, len(bin), size, "[]byte 长度必须精确")

		size, known = bodySize(txt)
		require.True(t, known, "string 必须判定为长度已知")
		require.EqualValues(t, len(txt), size, "string 长度必须精确")

		_, known = bodySize(nil)
		require.True(t, known, "nil 体应判定为已知且长度为零")

		_, known = bodySize(nopCloserBody{})
		require.False(t, known, "未知结构体类型必须判定为长度不可知")
	})
}
