package glog

import (
	"errors"
	"time"

	"gorm.io/gorm"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// dbQueryDuration 记录 DB 查询耗时（op, result）。
// op 从 SQL 首词提取（SELECT/INSERT/UPDATE/DELETE/OTHER），埋点在 glog.Trace
// 的日志级别检查**之前**——指标采集不应依赖日志开关。
var dbQueryDuration = pkgmetrics.HistogramDefault("db_query_duration_seconds", "Database query latency by operation and result.", []string{"op", "result"}...)

// extractSQLOp 从 SQL 语句提取操作类型（首词大写）。
// 先跳过前导空白，再取到第一个空白处为止的首词。
func extractSQLOp(sql string) string {
	// 跳过前导空白
	i := 0
	for i < len(sql) && (sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r') {
		i++
	}
	if i >= len(sql) {
		return "OTHER"
	}
	// 取首词（到下一个空白或结尾）
	start := i
	for i < len(sql) && sql[i] != ' ' && sql[i] != '\t' && sql[i] != '\n' && sql[i] != '\r' {
		i++
	}
	return normalizeSQLOp(sql[start:i])
}

func normalizeSQLOp(s string) string {
	switch {
	case len(s) == 0:
		return "OTHER"
	default:
		// 统一大写比较
		b := make([]byte, len(s))
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			b[i] = c
		}
		up := string(b)
		switch up {
		case "SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER":
			return up
		default:
			return "OTHER"
		}
	}
}

// observeDBQuery 记录一次 DB 查询耗时。
//
// 口径与 glog.Trace 的日志判定保持一致（glog.go:132）：
// gorm.ErrRecordNotFound 是"查不到记录"的正常结果（如 GetByID 未命中），
// 不是故障——若记 error 会把错误率打满、掩盖真实 DB 故障。
// 与 goredis 的 redis.Nil、cache 的 miss 口径一致。
func observeDBQuery(sql string, err error, cost time.Duration) {
	result := "ok"
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		result = "error"
	}
	dbQueryDuration.WithLabelValues(extractSQLOp(sql), result).Observe(cost.Seconds())
}
