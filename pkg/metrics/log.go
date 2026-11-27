package metrics

import (
	"fmt"
	"log"
	"sync/atomic"
)

// logWarnFn 报告器日志出口（atomic 保护：logger.Init 写一次，报告器 goroutine 并发读）。
//
// 为什么用 atomic：logger.Init 可在测试/重建场景多次调用，而报告器 goroutine
// 在后台持续读该函数——普通全局变量赋值会构成 data race（-race 可检出）。
// atomic.Pointer 保证写对读可见，且读路径无锁（日志在热路径外，但零成本更好）。
var logWarnFn atomic.Pointer[func(string)]

func init() {
	defaultFn := func(msg string) { log.Print(msg) }
	logWarnFn.Store(&defaultFn)
}

// SetLogWarnFn 注入结构化日志出口（由 logger.Init 调用）。
// 依赖倒置：pkg/metrics 不 import logger（避免 logger→metrics 反向依赖成环），
// 由 logger 侧注入，运行期报告日志即可进 SLS/requestID 体系。
func SetLogWarnFn(fn func(string)) {
	if fn == nil {
		return // 防误传 nil 导致 panic；保持现有出口
	}
	logWarnFn.Store(&fn)
}

// warnf 输出报告器 WARN 日志（统一前缀，走注入出口）。
func logWarnf(format string, args ...any) {
	(*logWarnFn.Load())("[metrics-report] WARN: " + fmt.Sprintf(format, args...))
}

// logWarnln 输出报告器 WARN 日志（无格式化参数版本）。
func logWarnln(msg string) {
	(*logWarnFn.Load())("[metrics-report] WARN: " + msg)
}

// logErrorf 输出报告器 ERROR 日志（走同一出口）。
func logErrorf(format string, args ...any) {
	(*logWarnFn.Load())("[metrics-report] ERROR: " + fmt.Sprintf(format, args...))
}
