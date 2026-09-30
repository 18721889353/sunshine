package logger

import (
	"os"
	"strings"
	"testing"
)

// TestMain 是包级测试入口，负责运行结束后的清理。
// 独立于 benchmark_test.go：避免该文件未来被加构建标签（如 //go:build !integration）或拆分时连带 TestMain 丢失，
// 也保证「同一包只能有一个 TestMain」这一约束有唯一、清晰的归属位置。
func TestMain(m *testing.M) {
	// 运行测试
	code := m.Run()

	// 仅在以基准模式运行（-test.bench=...）时才清理产物：
	// 避免 `go test -run TestXxx`（非基准）误删开发者手工放在 pkg/logger/ 下的同名文件
	if isBenchmarkRun(os.Args) {
		cleanupBenchmarkFiles()
	}

	os.Exit(code)
}

// isBenchmarkRun 判断本次 go test 是否以基准模式运行（go 会将 -bench 转为传入 -test.bench=<非空值>）
func isBenchmarkRun(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-test.bench=") && a != "-test.bench=" {
			return true
		}
	}
	return false
}

// cleanupBenchmarkFiles 清理基准测试生成的日志文件
// 注：go test 会保证测试二进制的工作目录为包目录，因此下方相对路径始终指向 pkg/logger/，
// 与基准中 WithFileName 的相对写入路径一致，不会误删其它目录文件
func cleanupBenchmarkFiles() {
	files := []string{
		"benchmark.log",
		"benchmark-error.log",
		"benchmark-module.log",
		"benchmark-sls.log",
		"benchmark-concurrent.log",
		"benchmark-sync.log",
		"benchmark-async.log",
		"benchmark-ctx.log",
		"benchmark-route.log",
		"benchmark-high-concurrency.log",
		"benchmark-mixed.log",
	}

	for _, file := range files {
		os.Remove(file)
	}

	dirs := []string{
		"logs/benchmark-order",
		"logs/benchmark-payment",
		"logs/benchmark-user",
	}

	for _, dir := range dirs {
		os.RemoveAll(dir)
	}
}
