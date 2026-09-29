package goredis

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ---------------------------------------------------------------------------
// 日志钩子测试辅助
// ---------------------------------------------------------------------------

// capturing 标记 Warn 钩子是否已被当前测试替换，用于拒绝嵌套与重复调用。
// 钩子是包级变量，两次 captureWarn 重叠时后者会把前者的钩子当作「原值」保存，
// 恢复后前者所属测试的断言将一直读到替换后的钩子，因此直接判定为测试用法错误。
var capturing atomic.Bool

// warnCollector 收集告警消息，内部用互斥锁保护。
//
// 当前本包的 Warn 调用均为同步路径，但钩子一旦未来被后台 goroutine 触发（如监控采集、
// 异步关闭），无锁读写会在 `-race` 下报错；测试辅助工具默认线程安全比依赖“调用方记住约束”更可靠。
type warnCollector struct {
	mu    sync.Mutex
	warns []string
}

// add 追加一条告警消息。
func (c *warnCollector) add(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warns = append(c.warns, msg)
}

// msgs 返回告警消息的快照副本，可安全在任意 goroutine 中调用。
func (c *warnCollector) msgs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	snapshot := make([]string, len(c.warns))
	copy(snapshot, c.warns)
	return snapshot
}

// captureWarn 临时替换 logging.go 中的包私有 Warn 日志钩子，收集告警消息供断言，
// 测试结束后自动还原。
//
// 钩子是包级变量，替换期间禁止并行测试（本包测试均未开启 t.Parallel），
// 且不支持在同一测试中嵌套/重复捕获（由 capturing 拦截）。
func captureWarn(t *testing.T) *warnCollector {
	t.Helper()

	if !capturing.CompareAndSwap(false, true) {
		t.Fatal("captureWarn 不支持在同一测试中嵌套或重复调用")
	}
	// 先注册标记释放 → 按 LIFO 最后执行，确保钩子已还原后才允许下一个测试捕获
	t.Cleanup(func() { capturing.Store(false) })

	collector := &warnCollector{}
	old := logWarn
	logWarn = func(_ context.Context, msg string, _ ...logger.Field) {
		collector.add(msg)
	}
	t.Cleanup(func() { logWarn = old })
	return collector
}

// funcPtr 返回函数的代码指针，用于判断钩子是否指向同一个实现（函数值不可直接比较）。
func funcPtr(f any) uintptr {
	return reflect.ValueOf(f).Pointer()
}

// ---------------------------------------------------------------------------
// 日志输出测试（全局 pkg/logger）
// ---------------------------------------------------------------------------

// TestWaitPoolDrained_超时告警走全局logger 验证等待连接池归还超时的告警经由全局 pkg/logger 输出。
func TestWaitPoolDrained_超时告警走全局logger(t *testing.T) {
	warns := captureWarn(t)

	// 模拟池内始终有命令占用连接（TotalConns != IdleConns）
	statsFn := func() *redis.PoolStats {
		return &redis.PoolStats{TotalConns: 2, IdleConns: 1}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitPoolDrained(ctx, statsFn)
	require.Error(t, err, "ctx 已取消应返回错误")

	msgs := warns.msgs()
	require.Len(t, msgs, 1, "等待超时告警应记录一条 Warn 日志")
	assert.Contains(t, msgs[0], "等待连接池归还超时")
}

// ---------------------------------------------------------------------------
// 日志钩子机制自身测试
// ---------------------------------------------------------------------------

// TestLogWarnHook_替换与还原 验证 Warn 钩子默认指向全局 pkg/logger，可被替换捕获并在测试结束后还原。
func TestLogWarnHook_替换与还原(t *testing.T) {
	defaultWarn := funcPtr(logger.WarnWithCtx)

	// t.Cleanup 是 LIFO：此处先注册最后执行，因此断言发生在 captureWarn 的还原逻辑之后，
	// 若调换注册顺序会读到尚未还原的钩子，导致测试恒失败。
	t.Cleanup(func() {
		assert.Equal(t, defaultWarn, funcPtr(logWarn), "测试结束后钩子应还原为全局 logger")
	})

	require.Equal(t, defaultWarn, funcPtr(logWarn), "钩子默认应指向全局 pkg/logger")

	warns := captureWarn(t)
	logWarn(context.Background(), "replaced", logger.Err(assert.AnError))
	require.Equal(t, []string{"replaced"}, warns.msgs(), "替换后应捕获到告警而非写入真实 logger")
}

// TestWarnCollector_并发安全 验证收集器在并发读写下不丢消息（钩子可能由后台 goroutine 触发）。
func TestWarnCollector_并发安全(t *testing.T) {
	collector := &warnCollector{}

	const perGoroutine = 50
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				collector.add("concurrent")
				_ = collector.msgs() // 并发读不应 panic 或干扰写入
			}
		}()
	}
	wg.Wait()

	require.Len(t, collector.msgs(), 2*perGoroutine, "并发写入的告警不应丢失")
}
