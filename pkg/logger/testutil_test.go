package logger

import "os"

// getEnv 获取环境变量，如果不存在则返回默认值
// 集中放置测试工具函数，供 method_ctx_test.go 与 benchmark_test.go 复用，
// 避免任一测试文件加构建标签或拆分后导致跨文件编译失败
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
