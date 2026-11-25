package generate

import (
	"strings"
	"testing"
)

// TestGetSubFiles_IncludesMetricsReporter 生成器必须自动带上 metrics_reporter.go。
//
// 历史教训类缺陷：模板 initial/initApp.go 调 config.StartMetricsReporter、
// initial/close.go 调 config.StopMetricsReporter，若 configAutoFiles 漏掉
// metrics_reporter.go，新生成的项目会因缺文件而编译失败。
// 本测试守护 configAutoFiles 与模板调用的契约。
func TestGetSubFiles_IncludesMetricsReporter(t *testing.T) {
	selectFiles := map[string][]string{
		"internal/config": {"serverNameExample.go"},
	}
	files := getSubFiles(selectFiles, nil)

	found := false
	for _, f := range files {
		if strings.HasSuffix(f, "internal/config/metrics_reporter.go") {
			found = true
			break
		}
	}
	if !found {
		t.Error("getSubFiles 应自动包含 internal/config/metrics_reporter.go（initApp/close 依赖，漏了新项目编译失败）")
	}

	// 热更新框架等既有文件也应仍在（防重构误删）
	for _, must := range []string{"reload.go", "reload_app.go"} {
		ok := false
		for _, f := range files {
			if strings.HasSuffix(f, "internal/config/"+must) {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("getSubFiles 应包含 internal/config/%s", must)
		}
	}
}

// TestGetSubFiles_NoDupWhenExplicit selectFiles 已显式列出时不重复追加。
func TestGetSubFiles_NoDupWhenExplicit(t *testing.T) {
	selectFiles := map[string][]string{
		"internal/config": {"serverNameExample.go", "metrics_reporter.go"},
	}
	files := getSubFiles(selectFiles, nil)

	count := 0
	for _, f := range files {
		if strings.HasSuffix(f, "internal/config/metrics_reporter.go") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("metrics_reporter.go 应恰好 1 次（去重失败）, got %d", count)
	}
}
