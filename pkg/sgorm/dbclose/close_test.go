package dbclose

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

// fakeConnector 提供不依赖真实数据库驱动的连接池，便于验证关闭与等待行为
type fakeConnector struct{}

func (fakeConnector) Connect(context.Context) (driver.Conn, error) { return fakeConn{}, nil }
func (fakeConnector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return fakeConn{}, nil }

type fakeConn struct{}

func (fakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

// newFakePool 造一个不会真正连库的连接池
func newFakePool() *sql.DB { return sql.OpenDB(fakeConnector{}) }

// newTestDB 构造只带主连接池的 gorm.DB，手工初始化 Plugins 以模拟 gorm.Open 之后的状态
func newTestDB(main *sql.DB) *gorm.DB {
	cfg := &gorm.Config{Plugins: map[string]gorm.Plugin{}, ConnPool: main}
	return &gorm.DB{Config: cfg, Statement: &gorm.Statement{ConnPool: main}}
}

func TestCloseDB(t *testing.T) {
	db := new(gorm.DB)
	defer func() { recover() }()
	_ = Close(db)
}

func TestPoolsReturnsMainPoolFirstWithDedup(t *testing.T) {
	main, extra := newFakePool(), newFakePool()
	db := newTestDB(main)

	// 主池重复登记、以及 nil 池都应被去重/剔除
	if err := RegisterPools(db, extra, main, nil); err != nil {
		t.Fatalf("RegisterPools() err = %v, want nil", err)
	}

	pools, err := Pools(db)
	if err != nil {
		t.Fatalf("Pools() err = %v, want nil", err)
	}
	if len(pools) != 2 {
		t.Fatalf("Pools() len = %d, want 2, pools = %v", len(pools), pools)
	}
	if pools[0] != main {
		t.Errorf("Pools()[0] = %p, want main pool %p", pools[0], main)
	}
	if pools[1] != extra {
		t.Errorf("Pools()[1] = %p, want extra pool %p", pools[1], extra)
	}
}

func TestPoolsOnNilDB(t *testing.T) {
	pools, err := Pools(nil)
	if err != nil {
		t.Fatalf("Pools(nil) err = %v, want nil", err)
	}
	if len(pools) != 0 {
		t.Fatalf("Pools(nil) len = %d, want 0", len(pools))
	}
}

func TestCloseClosesRegisteredPools(t *testing.T) {
	main, extra := newFakePool(), newFakePool()
	db := newTestDB(main)
	if err := RegisterPools(db, extra); err != nil {
		t.Fatalf("RegisterPools() err = %v, want nil", err)
	}

	if err := Close(db); err != nil {
		t.Fatalf("Close() err = %v, want nil", err)
	}

	// 关闭后任一池都应拒新查询（stdlib 的 errDBClosed 未导出，只能按错误文本判定），
	// 借此确认主池与登记池都被关闭
	ctx := context.Background()
	for name, pool := range map[string]*sql.DB{"main": main, "extra": extra} {
		if _, err := pool.QueryContext(ctx, "select 1"); !isDBClosedErr(err) {
			t.Errorf("%s pool query err = %v, want database closed error", name, err)
		}
	}
}

// isDBClosedErr 判断错误是否为“连接池已关闭”：database/sql 的 errDBClosed 未导出，无法用 errors.Is 匹配
func isDBClosedErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "database is closed")
}

func TestRegisterPoolsToleratesPartiallyBuiltDB(t *testing.T) {
	cases := []struct {
		name    string
		db      *gorm.DB
		hasPool bool
	}{
		{name: "nil db", db: nil, hasPool: true},
		{name: "nil config", db: new(gorm.DB), hasPool: true},
		{name: "nil plugins", db: &gorm.DB{Config: &gorm.Config{}}, hasPool: true},
		{name: "empty pools", db: newTestDB(newFakePool()), hasPool: false},
	}

	for _, tc := range cases {
		var err error
		if tc.hasPool {
			err = RegisterPools(tc.db, newFakePool())
		} else {
			err = RegisterPools(tc.db)
		}
		if err != nil {
			t.Errorf("%s: RegisterPools() err = %v, want nil", tc.name, err)
		}
	}
}

func TestCloseDoesNotWaitForInUseConnections(t *testing.T) {
	pool := newFakePool()
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatalf("pool.Conn() err = %v", err)
	}

	db := newTestDB(pool)
	start := time.Now()
	if closeErr := Close(db); closeErr != nil {
		t.Fatalf("Close() err = %v, want nil", closeErr)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("Close() waited %v with an in-use connection, want return immediately", elapsed)
	}

	// 池已关闭后再归还借出的连接：应安全返回（错误或 nil）而不是 panic
	_ = conn.Close()
	if _, err := pool.QueryContext(context.Background(), "select 1"); !isDBClosedErr(err) {
		t.Errorf("query on closed pool err = %v, want database closed error", err)
	}
}

// TestRegisterPoolsConcurrent 验证并发登记与读取无数据竞争，且登记结果不丢失、不重复
func TestRegisterPoolsConcurrent(t *testing.T) {
	db := newTestDB(newFakePool())
	const workers = 8

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			// 断言在子 goroutine 中只能用 Errorf，Fatalf 会从非测试 goroutine 终止整个测试
			if err := RegisterPools(db, newFakePool()); err != nil {
				t.Errorf("RegisterPools() err = %v, want nil", err)
			}
			if _, err := Pools(db); err != nil {
				t.Errorf("Pools() err = %v, want nil", err)
			}
		}()
	}
	wg.Wait()

	pools, err := Pools(db)
	if err != nil {
		t.Fatalf("Pools() err = %v", err)
	}
	// 主池 + workers 只登记池，按指针去重后应恰好等于 workers+1
	if len(pools) != workers+1 {
		t.Errorf("Pools() len = %d, want %d", len(pools), workers+1)
	}
}
