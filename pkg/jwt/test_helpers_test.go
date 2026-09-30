package jwt

import (
	"sync/atomic"
	"testing"
	"time"
)

// 本文件集中放置跨测试文件共享的测试工具（jwt_test.go / option_test.go /
// benchmark_test.go / fuzz_test.go 共用），避免同名 helper 在多个文件重复定义。
//
// 注意：文件名必须带 _test.go 后缀——本仓 .golangci.yml 配置了 run.tests: false，
// 非 _test.go 文件会被当作生产代码分析，仅测试使用的 helper 会被 unused 规则报错；
// 同时 _test.go 后缀保证 helper 不进入生产构建。

// testSigningKey 单元测试、基准测试、模糊测试统一使用的签名密钥。
// 值带 TEST_ONLY 前缀是刻意的：即使被误复制到配置文件里也能一眼识别为测试密钥。
const testSigningKey = "TEST_ONLY_do_not_use_in_production_qcGnoQoYKn9bWLFWjk7"

// testInUse 全局测试占用标志，拦截并发进入 initTestJWT 的调用（机制化保障，见 initTestJWT）。
var testInUse atomic.Bool

// initTestJWT 用固定的测试配置初始化全局 jwt，并在测试结束时还原调用前的配置，
// 避免用例之间通过全局状态互相污染。
//
// IMPORTANT（禁止并发）：本包测试共享同一份全局 optStore，
// 「Load 旧值 → 写入自己的配置 → Cleanup 还原」的模式在并发下会互相覆盖
// （A 保存的 prev 可能已含 B 的配置，Cleanup 时交叉污染）。
// 该约束不只靠注释：testInUse 以 CAS 拦截并发进入，误加 t.Parallel() 的用例
// 会在 initTestJWT 中直接 Fatal（而非静默污染或仅在 -race 下偶发失败）。
//
// 占用标志的复位依赖 Go testing 包的硬性保证：Cleanup 在测试结束时必然执行，
// 覆盖正常返回、t.Fatal / t.FailNow、以及用例内 panic 三条路径——因此只要
// CAS 成功并注册了 Cleanup，标志一定会释放；后到者 Fatal 时 Cleanup 尚未注册，
// 不会误清别人的标志，释放责任始终归属先行进入者。
// 如未来需真正并行，必须先改为每用例独立的配置注入机制（不走全局 optStore）。
func initTestJWT(tb testing.TB) {
	if !testInUse.CompareAndSwap(false, true) {
		tb.Fatalf("%s 检测到 initTestJWT 并发进入：本包测试共享全局 optStore，不支持 t.Parallel() 并行", tb.Name())
	}
	prev := optStore.Load()
	tb.Cleanup(func() {
		optStore.Store(prev)
		testInUse.Store(false)
	})

	Init(
		WithSigningKey(testSigningKey),
		WithExpire(time.Hour),
		WithIssuer("sunshine-test"),
	)
}
