package jwt

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 本文件是包的模糊测试，守护的是「不变量」而非具体输出值。
// 种子语料随常规 `go test` 执行；挖掘需显式 `go test -fuzz=<Target>`。

// FuzzParseTokenNeverPanics 验证不变量：ParseToken 对任意字符串不 panic；
// 解析失败时必返回 nil claims，解析成功时 claims 必非空且同一 token 可重复解析成功。
func FuzzParseTokenNeverPanics(f *testing.F) {
	mgr := newTestManager(f)

	// 种子语料：空串、半截结构、乱码、合法 token、被篡改的合法 token
	f.Add("")
	f.Add("abc")
	f.Add("xxx.xxx.xxx")
	valid, err := mgr.GenerateToken("10001", "sunshine", nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(valid + "tampered")
	// 真实签名篡改：翻转 payload 段中间一个字节，保留三段式结构——
	// 翻转结果可能仍是合法 base64url（走验签拒绝路径），也可能不是（走解码失败路径），
	// 两条路径的不变量都必须成立；区别于末尾追加字符（只会触发 base64 解码失败）
	if parts := strings.Split(valid, "."); len(parts) == 3 && len(parts[1]) > 0 {
		payload := []byte(parts[1])
		payload[len(payload)/2] ^= 0x01
		f.Add(parts[0] + "." + string(payload) + "." + parts[2])
	}

	f.Fuzz(func(t *testing.T, tokenString string) {
		claims, err := mgr.ParseToken(tokenString)
		if err != nil {
			if claims != nil {
				t.Fatalf("解析失败时必须返回 nil claims，实际返回 %#v", claims)
			}
			return
		}
		if claims == nil {
			t.Fatal("解析成功时 claims 不能为 nil")
		}
		// 解析是确定性的（种子 token 有效期为 1 小时，不存在两次调用间过期的边界），
		// 同一 token 第二次解析必须同样成功
		if _, err := mgr.ParseToken(tokenString); err != nil {
			t.Fatalf("同一 token 第二次解析必须成功，实际错误 %v", err)
		}
	})
}

// FuzzParseCustomTokenNeverPanics 验证不变量：ParseCustomToken 对任意字符串不 panic；
// 解析失败时必返回 nil claims，解析成功时 claims 必非空。
func FuzzParseCustomTokenNeverPanics(f *testing.F) {
	mgr := newTestManager(f)

	f.Add("")
	f.Add("abc")
	f.Add("xxx.xxx.xxx")
	valid, err := mgr.GenerateCustomToken(KV{"foo": "bar"})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(valid + "tampered")
	// 负值字段种子：守护「负数不得经 GetUint64 静默转换为无符号值」的不变量（数据损坏防护）
	negative, err := mgr.GenerateCustomToken(KV{"balance": -1, "foo": "bar"})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(negative)

	f.Fuzz(func(t *testing.T, tokenString string) {
		claims, err := mgr.ParseCustomToken(tokenString)
		if err != nil {
			if claims != nil {
				t.Fatalf("解析失败时必须返回 nil claims，实际返回 %#v", claims)
			}
			return
		}
		if claims == nil {
			t.Fatal("解析成功时 claims 不能为 nil")
		}
		// 不变量：负数字段（JSON 往返后为 float64）不得经 GetUint64 读取成功——
		// uint64 对负数的静默转换会得到巨大无符号值，属数据损坏而非读取成功
		for k := range claims.Fields {
			if raw, isNum := claims.Fields[k].(float64); isNum && raw < 0 {
				if _, ok := claims.GetUint64(k); ok {
					t.Fatalf("负数字段 %q 经 GetUint64 必须返回 false", k)
				}
			}
		}
	})
}

// FuzzTokenRoundTripFields 验证不变量：uid、name 为合法 UTF-8 时经签发 + 解析原样保留，
// 字符串字段原样保留，数字字段经 JSON 往返后为 float64 且能无损还原为原整数值。
//
// 不变量限定「合法 UTF-8」的原因：token 的 claims 走 encoding/json 序列化，
// 非法 UTF-8 字节会被强制替换为 U+FFFD（替换字符），往返后不再逐字节相等，
// 这是 JSON 层的既定行为，不是本包缺陷。
func FuzzTokenRoundTripFields(f *testing.F) {
	mgr := newTestManager(f)

	f.Add("10001", "sunshine")
	f.Add("", "")
	f.Add("中文 uid", "name with spaces")

	f.Fuzz(func(t *testing.T, uid, name string) {
		if !utf8.ValidString(uid) || !utf8.ValidString(name) {
			t.Skip("非合法 UTF-8 输入经 JSON 序列化会被替换为 U+FFFD，不适用本不变量")
		}

		fields := KV{"role": "admin", "level": 3}
		token, err := mgr.GenerateToken(uid, name, fields)
		if err != nil {
			t.Fatalf("签发失败: %v", err)
		}

		claims, err := mgr.ParseToken(token)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if claims.UID != uid {
			t.Fatalf("uid 未原样保留：%q != %q", claims.UID, uid)
		}
		if claims.Name != name {
			t.Fatalf("name 未原样保留：%q != %q", claims.Name, name)
		}
		if role, ok := claims.Fields["role"].(string); !ok || role != "admin" {
			t.Fatalf("字符串字段未原样保留：ok=%v val=%q", ok, role)
		}
		// JSON 数字统一反序列化为 float64，验证能无损还原为原整数值
		if level, ok := claims.Fields["level"].(float64); !ok || int(level) != 3 {
			t.Fatalf("数字字段未无损还原：ok=%v val=%v", ok, claims.Fields["level"])
		}
	})
}
