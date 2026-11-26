package metrics

import (
	"database/sql"
	"sync"

	"gorm.io/gorm"

	prom "github.com/prometheus/client_golang/prometheus"
)

// DB 连接池健康指标——连接池耗尽是典型"事前无信号"故障：
// 请求堆积、超时，但错误率可能还没起来。WaitCount/WaitDuration 是最早的预警。
var (
	descOpenConnections = prom.NewDesc("db_pool_open_connections", "Number of established DB connections.", []string{"db"}, nil)
	descInUse           = prom.NewDesc("db_pool_in_use_connections", "Number of connections currently in use.", []string{"db"}, nil)
	// 等待类指标带 result label（固定 error）：等待连接=连接池不够用=故障信号，
	// 诊断引擎靠 result!=ok 识别失败并告警。没有这个 label 就只展示不告警。
	descWaitCount    = prom.NewDesc("db_pool_wait_count_total", "Total number of connections waited for.", []string{"db", "result"}, nil)
	descWaitDuration = prom.NewDesc("db_pool_wait_duration_seconds_total", "Total time spent waiting for a connection.", []string{"db", "result"}, nil)
)

var dbPoolMu sync.Mutex
var registeredDBs = map[string]*sql.DB{}

func init() {
	// 注册 Collector：所有已登记的 DB 连接池统一在这里暴露。
	MustRegister(prom.CollectorFunc(collectDBPoolStats))
}

// RegisterDBStats 登记一个 DB 连接池供指标采集（幂等，重复登记同一 name 忽略）。
// name 用于 label 区分（如 "mysql"）。应在 DB 初始化后调用。
func RegisterDBStats(name string, db *gorm.DB) {
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	dbPoolMu.Lock()
	defer dbPoolMu.Unlock()
	if _, exists := registeredDBs[name]; exists {
		return
	}
	registeredDBs[name] = sqlDB
}

// collectDBPoolStats 实现 prometheus.Collector：每次 Gather 时读取各 DB 的 Stats。
// 用 CollectorFunc 避免自定义 Describe 的复杂性（无常量指标描述符，是 unchecked collector）。
func collectDBPoolStats(ch chan<- prom.Metric) {
	dbPoolMu.Lock()
	dbs := make(map[string]*sql.DB, len(registeredDBs))
	for k, v := range registeredDBs {
		dbs[k] = v
	}
	dbPoolMu.Unlock()

	for name, sqlDB := range dbs {
		st := sqlDB.Stats()
		// Stats() 的 WaitCount/WaitDuration 本身是累计值——必须直接 emit，
		// 不能 CounterVec.Add()（每次 Gather 都 Add 会让指标虚高）。
		ch <- prom.MustNewConstMetric(descOpenConnections, prom.GaugeValue, float64(st.OpenConnections), name)
		ch <- prom.MustNewConstMetric(descInUse, prom.GaugeValue, float64(st.InUse), name)
		ch <- prom.MustNewConstMetric(descWaitCount, prom.CounterValue, float64(st.WaitCount), name, labelError)
		ch <- prom.MustNewConstMetric(descWaitDuration, prom.CounterValue, st.WaitDuration.Seconds(), name, labelError)
	}
}
