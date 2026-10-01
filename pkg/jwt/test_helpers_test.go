package jwt

import (
	"testing"
	"time"
)

// 本文件集中放置跨测试文件共享的测试工具（jwt_test.go / benchmark_test.go /
// fuzz_test.go 共用），避免同名 helper 在多个文件重复定义。
//
// 注意：文件名必须带 _test.go 后缀——本仓 .golangci.yml 配置了 run.tests: false，
// 非 _test.go 文件会被当作生产代码分析，仅测试使用的 helper 会被 unused 规则报错；
// 同时 _test.go 后缀保证 helper 不进入生产构建。

// testSigningKey 单元测试、基准测试、模糊测试统一使用的签名密钥。
// 值带 TEST_ONLY 前缀是刻意的：即使被误复制到配置文件里也能一眼识别为测试密钥。
const testSigningKey = "TEST_ONLY_do_not_use_in_production_qcGnoQoYKn9bWLFWjk7"

// newTestManager 用固定的测试配置创建独立的 Manager 实例并返回。
//
// 实例模型带来的测试收益：每个用例持有自己的 Manager，配置互不共享，
// 用例之间天然隔离，不再需要旧全局单例时代的「占用标志 + Cleanup 还原」
// 串行约束；t.Parallel() 可安全使用，基准/模糊测试也不会互相污染配置。
//
// 基线配置：testSigningKey + 1 小时有效期 + sunshine-test 签发者；
// 追加 opts 通过 apply 叠加在基线上，同名选项最后赋值胜出（与生产侧 New 语义一致）。
// 需要「从默认值整体重建」语义时（如验证 Reload 不传选项会清空密钥），
// 请在用例内显式调用 mgr.Reload(...)，不要依赖本 helper。
func newTestManager(tb testing.TB, opts ...Option) *Manager {
	tb.Helper()
	base := []Option{
		WithSigningKey(testSigningKey),
		WithExpire(time.Hour),
		WithIssuer("sunshine-test"),
	}
	mgr, err := New(append(base, opts...)...)
	if err != nil {
		tb.Fatalf("创建测试 Manager 失败: %v", err)
	}
	return mgr
}
