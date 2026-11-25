package query

import (
	"strings"
	"testing"
	"time"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestQueryBuildMetricRegistered 指标进 Fast Registry（巡检可达）
func TestQueryBuildMetricRegistered(t *testing.T) {
	observeQueryBuild(nil, time.Millisecond)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "query_build_duration_seconds") {
			found = true
			break
		}
	}
	if !found {
		t.Error("query 指标应进 Fast Registry（否则 5s 巡检采不到）")
	}
}

// TestConvertToGormConditions_Build 埋点：成功与失败路径都记录
func TestConvertToGormConditions_Build(t *testing.T) {
	// 成功路径
	ok := &Params{Columns: []Column{{Name: "id", Exp: "=", Value: 1, Logic: ""}}}
	if _, _, err := ok.ConvertToGormConditions(); err != nil {
		t.Fatalf("正常条件应转换成功: %v", err)
	}
	// 失败路径（checkValid 失败）
	bad := &Params{Columns: []Column{{Name: "", Exp: "=", Value: 1}}}
	if _, _, err := bad.ConvertToGormConditions(); err == nil {
		t.Error("空列名应返回错误")
	}
}
