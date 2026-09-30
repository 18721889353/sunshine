package goredis

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// 本文件是 goredis 的模糊测试（Fuzz test），目标是把「不变量」交给随机输入反复冲击：
// DSN 解析不 panic 且不返回半成品、路径补全幂等、写入 Span 的文本必为合法 UTF-8。
//
// 运行方式：
//
//	# 只跑内置种子语料（默认随 go test 执行，耗时可忽略）
//	go test -run='^Fuzz' ./pkg/goredis/
//	# 真正的模糊挖掘（不进 CI 常规流程，按需本地跑）
//	go test -fuzz=FuzzGetRedisOptArbitraryDSN -fuzztime=30s ./pkg/goredis/
//	go test -fuzz=FuzzEnsureDSNPathIdempotent -fuzztime=30s ./pkg/goredis/
//	go test -fuzz=FuzzSpanTruncateTextValidUTF8 -fuzztime=30s ./pkg/goredis/
//
// 失败语料会写入 testdata/fuzz/<TargetName>/，需要人工确认后提交或修正。

// FuzzGetRedisOptArbitraryDSN 验证 DSN 解析的不变量：不 panic；
// 出错时返回 nil（避免调用方拿到半成品 Options 去建连接），成功时地址非空。
// DSN 来自配置文件/配置中心，是典型的「外部可控输入」，必须能被任意值击中。
func FuzzGetRedisOptArbitraryDSN(f *testing.F) {
	seeds := []string{
		"127.0.0.1:6379",
		"user:pass@127.0.0.1:6379/2",
		"redis://default:123456@localhost:6379/0?max_retries=3",
		"rediss://:pwd@tls.host:6379/1",
		"",
		"   ",
		"redis://",
		"://@/",
		"redis://p@ss%w@host:1/0",
		"redis://host:6379/0?timeout=1s&pool_size=abc",
		"\xff\xfe",
		strings.Repeat("a", 4096),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, dsn string) {
		opt, err := getRedisOpt(dsn, defaultOptions())

		if err != nil {
			require.Nil(t, opt, "解析失败时不应返回半成品 Options")
			require.Contains(t, err.Error(), "goredis:", "错误消息需带包名前缀，便于定位来源")
			return
		}
		require.NotNil(t, opt)
		require.NotEmpty(t, opt.Addr, "解析成功时地址必须非空，否则会连到语义不明的目标")
	})
}

// FuzzEnsureDSNPathIdempotent 验证 DSN 路径补全的不变量：不 panic、幂等、且不丢弃原 DSN 的任何部分。
// 幂等性是必需的前提——配置热更新时同一个 DSN 字符串可能被反复归一化。
func FuzzEnsureDSNPathIdempotent(f *testing.F) {
	seeds := []string{
		"redis://127.0.0.1:6379",
		"redis://127.0.0.1:6379/",
		"redis://127.0.0.1:6379/0",
		"redis://127.0.0.1:6379/?max_retries=3",
		"redis://user:pwd@host:6379/2?dial_timeout=3s",
		"redis:///",
		"127.0.0.1:6379",
		"",
		"://",
		"redis://a/b?c/d",
		strings.Repeat("?", 64),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, dsn string) {
		once := ensureDSNPath(dsn)
		twice := ensureDSNPath(once)
		require.Equal(t, once, twice, "路径补全必须幂等")

		if strings.Contains(dsn, "://") {
			require.Contains(t, once, "://", "补全不应丢失协议头")
			// 只做最小字符串拼接，不应删减或重排原有内容
			require.GreaterOrEqual(t, len(once), len(dsn), "补全只应增加字符，不应丢弃原内容")
		}
	})
}

// FuzzSpanTruncateTextValidUTF8 验证写入 Span 名称与属性的文本一定满足两条不变量：
//   - 合法 UTF-8：OTLP 的 protobuf string 字段要求合法 UTF-8，非法字节会让整批 Span 被后端拒绝或渲染成乱码；
//   - 长度受限：超长 key/锁名不得撑爆 Span 属性。
//
// Redis 的 key 常常由业务方拼接用户输入而来，属于外部可控数据，因此不能假设它一定是合法 UTF-8。
func FuzzSpanTruncateTextValidUTF8(f *testing.F) {
	seeds := []string{
		"user:1001:name",
		"lock:order:中文键名",
		"/dlock/my-lock-key",
		"\xff\xfe invalid",
		"a\x80b\x81c",
		strings.Repeat("汉", 200),
		"",
	}
	for _, seed := range seeds {
		f.Add(seed, 10)
	}

	f.Fuzz(func(t *testing.T, key string, limit int) {
		require.True(t, utf8.ValidString(truncateKey(key)), "db.redis.key 属性必须是合法 UTF-8")
		require.True(t, utf8.ValidString(trimLockName(key)), "Span 名称中的锁名必须是合法 UTF-8")
		require.True(t, utf8.ValidString(trimScriptSHA(key)), "Span 名称中的脚本摘要必须是合法 UTF-8")

		require.LessOrEqual(t, utf8.RuneCountInString(truncateKey(key)), maxKeyDisplayLen+3,
			"key 属性应被截断到上限长度（含省略号）")
		require.LessOrEqual(t, utf8.RuneCountInString(trimLockName(key)), maxLockNameLen,
			"锁名应被截断到上限长度")

		// 直接测试 truncateRunes：limit 归一到生产可达区间（调用方只传正常量常量），
		// 不把算力花在生产不可能出现的负值/天文数字上
		clamped := int(uint64(limit) % (maxKeyDisplayLen + 1))
		out, _ := truncateRunes(key, clamped)
		require.True(t, utf8.ValidString(out), "截断结果必须是合法 UTF-8")
		require.LessOrEqual(t, utf8.RuneCountInString(out), clamped, "截断结果不应超过 limit 个字符")
	})
}
