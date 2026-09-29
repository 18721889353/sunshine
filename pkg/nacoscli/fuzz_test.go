package nacoscli

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/stretchr/testify/require"

	"github.com/18721889353/sunshine/pkg/logger"
)

// 本文件是 nacoscli 的模糊测试（Fuzz test），目标是把「不变量」交给随机输入反复冲击：
// 参数校验不 panic、错误路径不带半截结果、零值/负值防护对所有输入成立。
//
// 运行方式：
//
//	# 只跑内置种子语料（默认随 go test 执行，耗时可忽略）
//	go test -run='^Fuzz' ./pkg/nacoscli/
//	# 真正的模糊挖掘（不进 CI 常规流程，按需本地跑）
//	go test -fuzz=FuzzParamsValid     -fuzztime=30s ./pkg/nacoscli/
//	go test -fuzz=FuzzClientGetConfig -fuzztime=30s ./pkg/nacoscli/
//	go test -fuzz=FuzzBuildOnChange   -fuzztime=30s ./pkg/nacoscli/
//	go test -fuzz=FuzzOptionZeroGuard -fuzztime=30s ./pkg/nacoscli/
//	go test -fuzz=FuzzSafeAttrText    -fuzztime=30s ./pkg/nacoscli/
//	go test -fuzz=FuzzWithSchemeWhitelist -fuzztime=30s ./pkg/nacoscli/
//
// 失败语料会写入 testdata/fuzz/<TargetName>/，需要人工确认后提交或修正。

// discardLogHooks 在模糊测试期间屏蔽包内日志钩子。
// 模糊挖掘会以极高频率重放输入，而 buildOnChange/GetConfig 的正常路径都会写日志，
// 不做屏蔽会把测试时间耗在日志 I/O 上（并填满临时目录）。
// 这里直接替换 logging.go 的包私有钩子，属于「测试专用」用法，约束见 logging.go 顶部说明。
func discardLogHooks(f *testing.F) {
	oldInfo, oldWarn, oldError, oldDebug := logInfo, logWarn, logError, logDebug
	noOp := func(context.Context, string, ...logger.Field) {}
	logInfo, logWarn, logError, logDebug = noOp, noOp, noOp, noOp
	f.Cleanup(func() {
		logInfo, logWarn, logError, logDebug = oldInfo, oldWarn, oldError, oldDebug
	})
}

// FuzzParamsValid 验证参数校验的不变量：不 panic、错误时返回空 format、
// 成功时 format 必为受支持的小写类型，且始终不修改入参 Params。
func FuzzParamsValid(f *testing.F) {
	seeds := [][3]string{
		{"DEFAULT_GROUP", "application.yaml", "yaml"},
		{"DEFAULT_GROUP", "application.yml", "yml"},
		{"DEFAULT_GROUP", "application.json", "JSON"},
		{"", "application.yaml", "yaml"},
		{"DEFAULT_GROUP", "", "yaml"},
		{"DEFAULT_GROUP", "application.toml", ""},
		{"DEFAULT_GROUP", "application.xml", "xml"},
		{"组\n名", "id\x00with\x00nul", "yaml"},
		{"DEFAULT_GROUP", "application.yaml", "yaml\n[error] 注入"},
		{strings.Repeat("g", 1024), strings.Repeat("d", 1024), strings.Repeat("y", 64)},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1], s[2])
	}

	f.Fuzz(func(t *testing.T, group, dataID, format string) {
		params := &Params{Group: group, DataID: dataID, Format: format}

		got, err := params.valid()

		if err != nil {
			require.Empty(t, got, "校验失败时不应返回任何 format")
			// Format 会拼入错误消息，必须已经转义控制字符，否则一行日志会被拆成多行（日志注入）
			require.NotContains(t, err.Error(), "\n", "错误消息不得含裸换行")
			require.NotContains(t, err.Error(), "\r", "错误消息不得含裸回车")
		} else {
			require.Contains(t, []string{"json", "yaml", "toml"}, got, "校验通过时 format 必须是受支持类型")
			require.Equal(t, strings.ToLower(got), got, "返回的 format 应为小写")
		}
		// valid() 只做校验与归一化，不得产生隐式副作用
		require.Equal(t, format, params.Format, "valid() 不应修改入参 Params.Format")
	})
}

// FuzzClientGetConfig 验证 Client.GetConfig 的不变量：非法参数与成功路径都不 panic，
// 出错时既不返回 format 也不返回内容（避免调用方误用半截配置）。
func FuzzClientGetConfig(f *testing.F) {
	discardLogHooks(f)

	seeds := [][3]string{
		{"DEFAULT_GROUP", "application.yaml", "yaml"},
		{"", "application.yaml", "yaml"},
		{"DEFAULT_GROUP", "", "yaml"},
		{"DEFAULT_GROUP", "application.xml", "xml"},
		{"DEFAULT_GROUP", "配置\n中心", "YAML"},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1], s[2])
	}

	client := &Client{configClient: &mockConfigClient{
		// 只依据入参派生返回值，不在 SDK 调用 goroutine 内断言，避免跨 goroutine 使用 testing.T
		getConfigFn: func(param vo.ConfigParam) (string, error) {
			return "content:" + param.Group + ":" + param.DataId, nil
		},
	}}

	f.Fuzz(func(t *testing.T, group, dataID, format string) {
		gotFormat, content, err := client.GetConfig(context.Background(), &Params{
			Group: group, DataID: dataID, Format: format,
		})

		if err != nil {
			require.Empty(t, gotFormat, "出错时不应返回 format")
			require.Nil(t, content, "出错时不应返回内容")
			return
		}
		require.Equal(t, "content:"+group+":"+dataID, string(content), "返回内容应与 SDK 结果一致")
	})
}

// FuzzBuildOnChange 验证配置变更回调的不变量：ctx 存活时 handler 恰好收到一次且参数原样透传，
// ctx 已取消时丢弃变更、绝不触达 handler。
func FuzzBuildOnChange(f *testing.F) {
	discardLogHooks(f)

	seeds := [][4]string{
		{"public", "DEFAULT_GROUP", "application.yaml", "key: value"},
		{"", "", "", ""},
		{"ns", "grp", "data", strings.Repeat("x", 4096)},
		{"ns\x00", "grp\n", "data\t", "中文配置值"},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1], s[2], s[3])
	}

	f.Fuzz(func(t *testing.T, namespace, group, dataID, data string) {
		var (
			calls int
			got   [4]string
		)
		listener := &ListenClient{
			configClient: &mockConfigClient{},
			handler: func(ns, g, id, d string) {
				calls++
				got = [4]string{ns, g, id, d}
			},
		}

		// 正常路径：ctx 存活
		ctx, cancel := context.WithCancel(context.Background())
		listener.buildOnChange(ctx)(namespace, group, dataID, data)
		cancel()
		require.Equal(t, 1, calls, "ctx 存活时 handler 应被调用一次")
		require.Equal(t, [4]string{namespace, group, dataID, data}, got, "回调参数应原样透传")

		// 取消路径：ctx 已取消，丢弃变更且不触达 handler
		canceledCtx, cancel2 := context.WithCancel(context.Background())
		cancel2()
		calls = 0
		listener.buildOnChange(canceledCtx)(namespace, group, dataID, data)
		require.Zero(t, calls, "ctx 已取消时不应调用 handler")
	})
}

// FuzzOptionZeroGuard 验证所有 Option 的零值/负值防护对任意输入成立：
// 无论传入什么，timeoutMs / createDelay / getTimeout 必须始终为正（否则分别等价于立即超时与忙循环），
// port / maxRetries 必须非负。这是 WatchConfig 与 GetConfig 不会打满 CPU 或必然失败的前提。
func FuzzOptionZeroGuard(f *testing.F) {
	seeds := []struct {
		port          int
		timeoutMs     int
		maxRetries    int
		createDelayNs int64
		getTimeoutNs  int64
	}{
		{8848, 5000, 3, int64(5 * time.Second), int64(30 * time.Second)},
		{0, 0, 0, 0, 0},
		{-1, -1, -1, -1, -1},
		{65536, 1 << 30, 1 << 30, int64(^uint64(0) >> 1), int64(^uint64(0) >> 1)},
		{-65536, -9223372036854775808, -9223372036854775808, -9223372036854775808, -9223372036854775808},
	}
	for _, s := range seeds {
		f.Add(s.port, s.timeoutMs, s.maxRetries, s.createDelayNs, s.getTimeoutNs)
	}

	f.Fuzz(func(t *testing.T, port, timeoutMs, maxRetries int, createDelayNs, getTimeoutNs int64) {
		o := defaultOptions()
		o.apply(
			WithPort(port),
			WithTimeoutMs(timeoutMs),
			WithMaxRetries(maxRetries),
			WithCreateDelay(time.Duration(createDelayNs)),
			WithGetTimeout(time.Duration(getTimeoutNs)),
		)

		require.Positive(t, o.timeoutMs, "timeoutMs 非正值会令 SDK 行为未定义，必须被忽略")
		require.Positive(t, o.createDelay, "createDelay 非正值会让 time.After(0) 忙循环，必须被忽略")
		require.Positive(t, o.getTimeout, "getTimeout 非正值会让 context 立即到期，必须被忽略")
		require.GreaterOrEqual(t, o.port, 0, "port 不应为负")
		require.GreaterOrEqual(t, o.maxRetries, 0, "maxRetries 不应为负（0 表示无限重试）")
	})
}

// FuzzSafeAttrText 验证写入 Span 属性前的 UTF-8 归一化不变量：
// 输出必为合法 UTF-8；合法输入必须原样返回（归一化不得改写正常配置名）。
// Group/DataID 来自配置文件与配置中心，与 pkg/goredis 的 Redis key 属同一类外部可控输入。
func FuzzSafeAttrText(f *testing.F) {
	seeds := []string{
		"application.yaml",
		"配置中心.yaml",
		"",
		"\xff\xfe invalid",
		"a\x80b\x81c",
		"id\x00with\x00nul",
		strings.Repeat("汉", 200),
		strings.Repeat("\xff", 64),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got := safeAttrText(s)
		require.True(t, utf8.ValidString(got), "归一化结果必须是合法 UTF-8，否则 OTLP 会整批丢弃 Span")

		if utf8.ValidString(s) {
			require.Equal(t, s, got, "合法 UTF-8 输入必须原样返回，不得被改写")
			return
		}
		require.NotEqual(t, s, got, "非法输入必须被替换")
	})
}

// FuzzWithSchemeWhitelist 验证 WithScheme 的白名单不变量：
// 任意输入（含任意大小写/空格/非法协议名）施加后，scheme 只能是未设置或 http/https/grpc，
// 且必为小写无首尾空格——不存在「配置里的拼写错误原样透传给 SDK」这条路径。
func FuzzWithSchemeWhitelist(f *testing.F) {
	seeds := []string{"http", "HTTPS", " grpc ", "h2", "tcp", "", "   ", "http\n", "\xff", strings.Repeat("a", 128)}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, scheme string) {
		o := defaultOptions()
		o.scheme = "grpc" // 先前值：非白名单输入应保留它
		WithScheme(scheme)(o)

		require.Contains(t, []string{"", "http", "https", "grpc"}, o.scheme, "scheme 只能是未设置或白名单值")
		require.Equal(t, strings.ToLower(o.scheme), o.scheme, "scheme 应为小写")
		require.Equal(t, strings.TrimSpace(o.scheme), o.scheme, "scheme 不应含首尾空格")
	})
}
