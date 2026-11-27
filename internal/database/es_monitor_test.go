package database

import "testing"

// TestCloseElasticsearch_NotInitialized 未初始化 ES 时关闭应安全（优雅停机无差别调用）。
func TestCloseElasticsearch_NotInitialized(t *testing.T) {
	// 确保本次用例从未初始化 monitor（其他测试可能设过，先清空）
	esMonitorCancel.Store(nil)
	if err := CloseElasticsearch(); err != nil {
		t.Errorf("未初始化时关闭不应报错, got %v", err)
	}
	// 重复关闭幂等
	if err := CloseElasticsearch(); err != nil {
		t.Errorf("重复关闭不应报错, got %v", err)
	}
}
