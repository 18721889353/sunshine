package metrics

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestRegisterDBStats_Collect 验证 DB 连接池指标能被 Collector emit——
// 且重复登记同一 name 幂等（不产生重复 metrics 冲突）。
func TestRegisterDBStats_Collect(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mock.ExpectClose()

	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	RegisterDBStats("test-db", db)
	RegisterDBStats("test-db", db) // 幂等
	t.Cleanup(func() {
		dbPoolMu.Lock()
		delete(registeredDBs, "test-db")
		dbPoolMu.Unlock()
	})

	// 通过 Default().GatherDefault() 验证指标可见（含自定义 Collector）
	families, err := Default().GatherDefault()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	found := map[string]bool{}
	for _, f := range families {
		switch f.GetName() {
		case "db_pool_open_connections", "db_pool_in_use_connections",
			"db_pool_wait_count_total", "db_pool_wait_duration_seconds_total":
			found[f.GetName()] = true
			// 等待类指标必须带 result=error（进诊断），连接数类不带
			if f.GetName() == "db_pool_wait_count_total" || f.GetName() == "db_pool_wait_duration_seconds_total" {
				for _, m := range f.GetMetric() {
					hasResult := false
					for _, l := range m.GetLabel() {
						if l.GetName() == "result" && l.GetValue() == "error" {
							hasResult = true
						}
					}
					if !hasResult {
						t.Errorf("%s 缺少 result=error label，不会进诊断", f.GetName())
					}
				}
			}
		}
	}
	for _, name := range []string{"db_pool_open_connections", "db_pool_in_use_connections",
		"db_pool_wait_count_total", "db_pool_wait_duration_seconds_total"} {
		if !found[name] {
			t.Errorf("指标 %s 未被 Collector emit", name)
		}
	}
}
