package nacoscli

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ---------------------------------------------------------------------------
// 日志钩子测试辅助
// ---------------------------------------------------------------------------

// capturing 标记日志钩子是否已被当前测试替换，用于拒绝嵌套与重复调用。
// 钩子是包级变量，两次 captureLogs 重叠时，后者会把前者的钩子当作「原值」保存，
// 恢复后前者所属测试的断言将一直读到替换后的钩子，因此直接判定为测试用法错误。
var capturing atomic.Bool

// logCollector 按级别收集日志，内部用互斥锁保护。
//
// 写入方可能是后台 goroutine（如 Nacos SDK 的 ListenConfig 回调，见 listener.go buildOnChange），
// 读取方是测试主 goroutine，两者之间没有可靠的 happens-before 保证，
// 因此写入与读取都必须过锁，否则 `-race` 下会报 concurrent map writes / read-write race。
type logCollector struct {
	mu   sync.Mutex
	logs map[string][]string
}

// add 追加一条指定级别的日志消息。
func (c *logCollector) add(level, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs[level] = append(c.logs[level], msg)
}

// msgs 返回指定级别日志的快照副本，可安全在任意 goroutine 中调用。
func (c *logCollector) msgs(level string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	msgs := c.logs[level]
	snapshot := make([]string, len(msgs))
	copy(snapshot, msgs)
	return snapshot
}

// captureLogs 临时替换 logging.go 中的包私有日志钩子，按级别收集日志消息，
// 测试结束后自动还原（t.Cleanup 在测试函数返回后执行，即 stop() 等异步流程结束之后）。
//
// 返回的 logCollector 内部带锁，可以在异步流程尚未完全结束时安全读取；
// 但断言「日志是否已产生」仍需等被测流程真正结束（如调用 stop()）后再读，
// 否则读到的是中途快照。
//
// 钩子是包级变量，替换期间禁止并行测试（本包测试均未开启 t.Parallel），
// 且不支持在同一测试中嵌套/重复调用（由 capturing 拦截）。
func captureLogs(t *testing.T) *logCollector {
	t.Helper()

	if !capturing.CompareAndSwap(false, true) {
		t.Fatal("captureLogs 不支持在同一测试中嵌套或重复调用")
	}
	// 先注册标记释放 → 按 LIFO 最后执行，确保钩子已还原后才允许下一个测试捕获
	t.Cleanup(func() { capturing.Store(false) })

	collector := &logCollector{logs: make(map[string][]string)}
	// hook 生成指定级别的替换实现，仅保留消息文本，忽略具体字段
	hook := func(level string) func(context.Context, string, ...logger.Field) {
		return func(_ context.Context, msg string, _ ...logger.Field) {
			collector.add(level, msg)
		}
	}

	oldDebug, oldInfo, oldWarn, oldError := logDebug, logInfo, logWarn, logError
	logDebug, logInfo, logWarn, logError = hook("debug"), hook("info"), hook("warn"), hook("error")
	t.Cleanup(func() {
		logDebug, logInfo, logWarn, logError = oldDebug, oldInfo, oldWarn, oldError
	})
	return collector
}

// funcPtr 返回函数的代码指针，用于判断钩子是否指向同一个实现（函数值不可直接比较）。
func funcPtr(f any) uintptr {
	return reflect.ValueOf(f).Pointer()
}

// ---------------------------------------------------------------------------
// ListenClient 日志输出测试
// ---------------------------------------------------------------------------

// TestListenClientStartLogsViaGlobalLogger 验证 ListenClient.Start 的日志经由全局 pkg/logger 输出。
// 场景：ListenConfig 注册失败，应先记录启动 Info，再记录注册失败 Warn。
func TestListenClientStartLogsViaGlobalLogger(t *testing.T) {
	logs := captureLogs(t)

	mock := &mockConfigClient{
		listenConfigFn: func(_ vo.ConfigParam) error {
			return fmt.Errorf("SDK 内部错误")
		},
	}
	listener := &ListenClient{
		configClient: mock,
		group:        "g",
		dataID:       "d",
		handler:      func(_, _, _, _ string) {},
		param:        vo.ConfigParam{DataId: "d", Group: "g"},
	}

	err := listener.Start(context.Background())
	require.Error(t, err, "ListenConfig 失败应返回错误")

	assert.NotEmpty(t, logs.msgs("info"), "启动应记录 Info 日志")
	assert.NotEmpty(t, logs.msgs("warn"), "注册监听失败应记录 Warn 日志")
}

// TestSafeCallHandlerPanicLogsViaGlobalLogger 验证 handler panic 被 recover 后经全局 logger 记录 Warn。
func TestSafeCallHandlerPanicLogsViaGlobalLogger(t *testing.T) {
	logs := captureLogs(t)

	listener := &ListenClient{
		handler: func(_, _, _, _ string) { panic("boom") },
	}
	listener.safeCallHandler(context.Background(), "ns", "g", "d", "data")

	warns := logs.msgs("warn")
	require.Len(t, warns, 1, "panic 应恰好记录一条 Warn 日志")
	assert.Contains(t, warns[0], "回调 panic")
}

// ---------------------------------------------------------------------------
// 日志钩子机制自身测试
// ---------------------------------------------------------------------------

// TestLogHooksReplaceAndRestore 验证日志钩子默认指向全局 pkg/logger，可被替换捕获并在测试结束后还原。
func TestLogHooksReplaceAndRestore(t *testing.T) {
	defaultWarn, defaultError := funcPtr(logger.WarnWithCtx), funcPtr(logger.ErrorWithCtx)

	// t.Cleanup 是 LIFO：此处先注册最后执行，因此断言发生在 captureLogs 的还原逻辑之后，
	// 若调换注册顺序会读到尚未还原的钩子，导致测试恒失败。
	t.Cleanup(func() {
		assert.Equal(t, defaultWarn, funcPtr(logWarn), "测试结束后 Warn 钩子应还原为全局 logger")
		assert.Equal(t, defaultError, funcPtr(logError), "测试结束后 Error 钩子应还原为全局 logger")
	})

	require.Equal(t, defaultWarn, funcPtr(logWarn), "钩子默认应指向全局 pkg/logger")

	logs := captureLogs(t)
	logWarn(context.Background(), "replaced", logger.String("k", "v"))
	require.Equal(t, []string{"replaced"}, logs.msgs("warn"), "替换后应捕获到日志而非写入真实 logger")
}

// TestLogCollectorConcurrentAdd 验证 logCollector 在并发写入下不丢消息、不加锁失败 panic。
// 对应场景：Nacos SDK 后台回调 goroutine 与测试主 goroutine 同时写日志。
func TestLogCollectorConcurrentAdd(t *testing.T) {
	collector := &logCollector{logs: make(map[string][]string)}

	const perGoroutine = 50
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				collector.add("warn", "concurrent")
				_ = collector.msgs("info") // 并发读不应 panic 或干扰写入
			}
		}()
	}
	wg.Wait()

	require.Len(t, collector.msgs("warn"), 2*perGoroutine, "并发写入的消息不应丢失")
}
