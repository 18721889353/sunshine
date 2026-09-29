package goredis

import (
	"github.com/18721889353/sunshine/pkg/logger"
)

// 本包的日志统一走全局 pkg/logger（zap 底座、ctx 感知、自带 request_id 关联），
// 与仓内 es / etcdcli / nacoscli / goMq / gin middleware / cache 等包的用法保持一致。
//
// 历史遗留：本包曾提供 Logger 接口 + WithLogger 注入（默认实现走标准库 slog），
// 但生产代码从未注入过，默认实现还会绕过项目日志管道，形成日志盲区，故已移除。
// 若将来需要替换底层日志实现，正确的抽象点是 pkg/logger 自身（统一收口），
// 而不是每个基础设施包各造一套 Logger + WithLogger。
//
// 下面的包私有变量仅用于单元测试替换以捕获日志，不对外暴露。
//
// 替换该变量等价于修改全局状态，使用约束（由 logging_test.go 的 captureWarn 强制）：
//   - 生产代码一律视为只读，不得提供 SetLogger 类的运行时替入入口；
//   - 替换期间禁止 t.Parallel()，否则并行测试会读到彼此的钩子；
//   - 禁止在同一测试中嵌套/重复捕获（否则会把上一个钩子当成默认值保存）；
//   - 测试侧收集日志必须用带锁的收集器（见 logging_test.go 的 warnCollector），
//     避免钩子未来由后台 goroutine 触发时产生 data race。
//
// 本包当前仅产生 Warn 级别日志（探测失败 / 初始化失败后关闭出错 / 连接池未排空），
// 需要其他级别时在对应调用点直接使用 logger.DebugWithCtx / InfoWithCtx / ErrorWithCtx。
var logWarn = logger.WarnWithCtx
