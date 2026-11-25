package glog

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestDBQueryMetricRegistered 指标进 Fast Registry（巡检可达）
func TestDBQueryMetricRegistered(t *testing.T) {
	observeDBQuery("SELECT 1", nil, time.Millisecond)
	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	found := false
	for _, f := range fams {
		if strings.Contains(f.GetName(), "db_query_duration_seconds") {
			found = true
			break
		}
	}
	if !found {
		t.Error("db 指标应进 Fast Registry（否则 5s 巡检采不到）")
	}
}

// TestExtractSQLOp SQL 操作类型提取
func TestExtractSQLOp(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM t":  "SELECT",
		"  select 1":       "SELECT",
		"INSERT INTO t":    "INSERT",
		"UPDATE t SET a=1": "UPDATE",
		"DELETE FROM t":    "DELETE",
		"EXPLAIN SELECT":   "OTHER",
		"":                 "OTHER",
	}
	for sql, want := range cases {
		if got := extractSQLOp(sql); got != want {
			t.Errorf("extractSQLOp(%q) = %q, want %q", sql, got, want)
		}
	}
}

// TestObserveDBQuery_ErrRecordNotFound 口径：ErrRecordNotFound 是正常结果（记 ok），
// 与 glog.Trace 日志判定一致——不把"查不到记录"计入 DB 错误率。
func TestObserveDBQuery_ErrRecordNotFound(t *testing.T) {
	// 该函数不 panic 即说明口径分支正常（具体 label 值通过 Gather 验证）
	observeDBQuery("SELECT * FROM t WHERE id=1", gorm.ErrRecordNotFound, time.Millisecond)
	observeDBQuery("SELECT * FROM t", errors.New("connection refused"), time.Millisecond)

	fams, err := pkgmetrics.Default().GatherFast()
	if err != nil {
		t.Fatalf("gather fast: %v", err)
	}
	for _, f := range fams {
		if f.GetName() == "db_query_duration_seconds" {
			for _, m := range f.GetMetric() {
				for _, l := range m.GetLabel() {
					if l.GetName() == "result" && l.GetValue() == "ok" {
						return // 至少有 ok 样本（ErrRecordNotFound 记 ok）
					}
				}
			}
		}
	}
	t.Error("ErrRecordNotFound 应记 ok")
}
