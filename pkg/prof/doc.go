// Package prof 封装官方 net/http/pprof 路由和运行时 profiling 采样能力，提供大厂标准化的性能分析工具集。
//
// # 功能特性
//
//  1. HTTP 实时分析：将 pprof 路由注册到 http.ServeMux 或 Gin 引擎，
//     通过浏览器或 go tool pprof 实时查看 CPU、内存、协程等 profile 数据。
//     支持可选鉴权中间件保护生产环境安全。
//
//  2. 信号触发采样：通过 NewProfile() 创建采样器，结合系统信号（SIGTRAP）开关
//     采样。适合生产环境按需采集，不影响正常服务性能。
//
//  3. 自适应采集：结合资源监控告警，在 CPU/内存超阈值时自动触发 profile 采样，
//     便于问题事后追溯。
//
// # 采集类型
//
//   - CPU：CPU 使用率 profiling，分析热点函数
//   - Memory：堆内存分配，定位内存泄漏
//   - Goroutine：所有 goroutine 堆栈，排查协程泄漏
//   - Block：同步原语阻塞，分析锁竞争
//   - Mutex：互斥锁持有者，排查死锁
//   - ThreadCreate：线程创建，分析线程爆炸
//   - Trace：运行时 trace（可选），分析调度和 GC
//
// # 使用方式
//
// HTTP 方式：
//
//	mux := http.NewServeMux()
//	prof.Register(mux, prof.WithIOWaitTime())
//
// 信号触发方式：
//
//	p := prof.NewProfile(prof.WithProfileDuration(30))
//	// 收到 SIGTRAP 时调用 p.StartOrStop()
//
// # 安全提示
//
// pprof 可能暴露敏感信息（源码路径、goroutine 堆栈等），
// 生产环境建议：
//   - 通过 WithAuth() 添加鉴权中间件
//   - 仅在内部网络暴露 pprof 端口
//   - 结合 K8S 网络安全策略限制访问
package prof
