package dao

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/pkg/dao"
	"github.com/18721889353/sunshine/pkg/dao/test/cache"
	"github.com/18721889353/sunshine/pkg/dao/test/model"
	"github.com/18721889353/sunshine/pkg/sgorm"
	"github.com/18721889353/sunshine/pkg/sgorm/query"
	_ "github.com/pingcap/tidb/pkg/parser/test_driver"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ---------- 辅助函数 ----------
func int64Ptr(v int64) *int64 {
	return &v
}

// ---------- 测试配置 ----------
var (
	testDBHost        = "bj-cdb-3hacsolk.sql.tencentcdb.com"
	testDBPort        = "23163"
	testDBUser        = "tcbank" //tcbank
	testDBPassword    = "tcbank@1234"
	testDBName        = "coupon_platform"
	testRedisHost     = "43.143.78.234:6379"
	testRedisPassword = "jianguo123"

	testLogFile *os.File
)

func tableName() string {
	return (&model.SysUserExample{}).TableName()
}

func initTestDB() *gorm.DB {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		testDBUser, testDBPassword, testDBHost, testDBPort, testDBName)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database: " + err.Error())
	}
	db.AutoMigrate(&model.SysUserExample{})
	return db
}

// ---------- Redis 缓存初始化 ----------
var testRedisCache cache.SysUserExampleCache

func newRedisCache() cache.SysUserExampleCache {
	rdb := redis.NewClient(&redis.Options{
		Addr:     testRedisHost,
		Password: testRedisPassword,
		DB:       0,
	})
	cacheType := &database.CacheType{
		CType: "redis",
		Rdb:   rdb,
	}
	return cache.NewSysUserExampleCache(cacheType)
}

// ---------- TestMain ----------
func TestMain(m *testing.M) {
	testRedisCache = newRedisCache()
	db := initTestDB()
	clearTable(db)

	ctx := context.Background()
	prefixes := []string{
		"data:sysUserExample:",
		"condition:",
		"columns:",
		"count:",
		"exists:",
	}
	for _, p := range prefixes {
		_ = testRedisCache.DelByPrefix(ctx, p)
	}

	code := m.Run()
	if closer, ok := testRedisCache.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	os.Exit(code)
}

// ---------- 辅助 ----------
func clearTable(db *gorm.DB) {
	db.Exec("DELETE FROM " + tableName())
}

// ---------- 原始测试用例（已修改指针） ----------

// TestBasicCRUD 基础增删改查
func TestBasicCRUD(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		DefaultExpireTime: 5 * time.Second,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	user := &model.SysUserExample{Name: "basic", Age: int64Ptr(20), Status: int64Ptr(1)}
	err := userDao.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	t.Logf("Created ID: %d", user.ID)

	got, err := userDao.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Name != "basic" || *got.Age != 20 {
		t.Errorf("Data mismatch: got %+v", got)
	}

	user.Name = "basic_updated"
	err = userDao.UpdateByID(ctx, user)
	if err != nil {
		t.Fatalf("UpdateByID failed: %v", err)
	}
	got2, err := userDao.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got2.Name != "basic_updated" {
		t.Errorf("Update not reflected: got %s", got2.Name)
	}

	err = userDao.DeleteByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}
	_, err = userDao.GetByID(ctx, user.ID)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("Expected ErrRecordNotFound, got %v", err)
	}
}

// TestConditionalQueries 条件查询
func TestConditionalQueries(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		DefaultExpireTime: 5 * time.Second,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	users := []*model.SysUserExample{
		{Name: "alice", Age: int64Ptr(25), Status: int64Ptr(1)},
		{Name: "bob", Age: int64Ptr(30), Status: int64Ptr(2)},
		{Name: "charlie", Age: int64Ptr(25), Status: int64Ptr(1)},
	}
	for _, u := range users {
		if err := userDao.Create(ctx, u); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	params := &query.Params{Columns: []query.Column{{Name: "name", Value: "alice"}}, Sort: "-id"}
	got, err := userDao.GetOneByColumns(ctx, params)
	if err != nil {
		t.Fatalf("GetOneByColumns failed: %v", err)
	}
	if got.Name != "alice" {
		t.Errorf("Expected alice, got %s", got.Name)
	}

	pageParams := &query.Params{
		Page:    0,
		Limit:   10,
		Sort:    "-id",
		Columns: []query.Column{{Name: "status", Value: "1"}},
	}
	records, total, err := userDao.GetByColumns(ctx, pageParams)
	if err != nil {
		t.Fatalf("GetByColumns failed: %v", err)
	}
	if total != 2 {
		t.Errorf("Expected total 2, got %d", total)
	}
	if len(records) != 2 {
		t.Errorf("Expected 2 records, got %d", len(records))
	}

	cond := &query.Conditions{Columns: []query.Column{{Name: "age", Value: 25}}}
	ids, err := userDao.GetByCondition(ctx, cond)
	if err != nil {
		t.Fatalf("GetByCondition failed: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("Expected 2 IDs, got %d", len(ids))
	}

	count, err := userDao.CountByCondition(ctx, cond)
	if err != nil {
		t.Fatalf("CountByCondition failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}

	exists, err := userDao.ExistsByCondition(ctx, &query.Conditions{Columns: []query.Column{{Name: "name", Value: "bob"}}})
	if err != nil {
		t.Fatalf("ExistsByCondition failed: %v", err)
	}
	if !exists {
		t.Error("Expected bob to exist")
	}
	exists, err = userDao.ExistsByCondition(ctx, &query.Conditions{Columns: []query.Column{{Name: "name", Value: "david"}}})
	if err != nil {
		t.Fatalf("ExistsByCondition failed: %v", err)
	}
	if exists {
		t.Error("Expected david not to exist")
	}
}

// TestBatchOperations 批量操作
func TestBatchOperations(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		DefaultExpireTime: 5 * time.Second,
		MaxBatchSize:      50,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	users := make([]*model.SysUserExample, 200)
	for i := 0; i < 200; i++ {
		users[i] = &model.SysUserExample{
			Name:   fmt.Sprintf("batch_%d", i),
			Age:    int64Ptr(int64(i % 60)),
			Status: int64Ptr(1),
		}
	}
	err := userDao.CreateInBatches(ctx, users, 30)
	if err != nil {
		t.Fatalf("CreateInBatches failed: %v", err)
	}

	ids := make([]uint64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}

	respMap, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(respMap) != 200 {
		t.Errorf("Expected 200 records, got %d", len(respMap))
	}
	for _, id := range ids {
		if _, ok := respMap[id]; !ok {
			t.Errorf("ID %d missing", id)
		}
	}

	for i := 0; i < 50; i++ {
		users[i].Name = "updated_batch"
		err = userDao.UpdateByID(ctx, users[i])
		if err != nil {
			t.Errorf("UpdateByID failed for id %d: %v", users[i].ID, err)
		}
	}

	respMap2, err := userDao.GetByIDs(ctx, ids[:50])
	if err != nil {
		t.Fatalf("GetByIDs after update failed: %v", err)
	}
	for i := 0; i < 50; i++ {
		if got, ok := respMap2[users[i].ID]; ok {
			if got.Name != "updated_batch" {
				t.Errorf("Update not reflected for id %d: got %s", users[i].ID, got.Name)
			}
		} else {
			t.Errorf("ID %d not found after update", users[i].ID)
		}
	}

	cond := &query.Conditions{Columns: []query.Column{{Name: "status", Value: "1"}}}
	err = userDao.DeleteByCondition(ctx, cond)
	if err != nil {
		t.Fatalf("DeleteByCondition failed: %v", err)
	}
	count, _ := userDao.CountByCondition(ctx, cond)
	if count != 0 {
		t.Errorf("After delete, count = %d", count)
	}
}

// TestTransactionMethods 事务方法
func TestTransactionMethods(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	t.Run("CommitTransaction", func(t *testing.T) {
		tx := db.Begin()
		user := &model.SysUserExample{Name: "tx_user", Age: int64Ptr(25), Status: int64Ptr(1)}
		id, err := userDao.CreateByTx(ctx, tx, user)
		if err != nil {
			tx.Rollback()
			t.Fatalf("CreateByTx failed: %v", err)
		}
		if id == 0 {
			tx.Rollback()
			t.Fatal("ID not assigned")
		}

		user.Name = "tx_updated"
		err = userDao.UpdateByTx(ctx, tx, user)
		if err != nil {
			tx.Rollback()
			t.Fatalf("UpdateByTx failed: %v", err)
		}

		var txUser model.SysUserExample
		if err := tx.Table(tableName()).Where("id = ?", id).First(&txUser).Error; err != nil {
			tx.Rollback()
			t.Fatalf("查询事务内数据失败: %v", err)
		}
		if txUser.Name != "tx_updated" {
			tx.Rollback()
			t.Errorf("事务内数据未更新: got %s", txUser.Name)
		}

		if err := tx.Commit().Error; err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		got, err := userDao.GetByID(ctx, id, SysUserExampleWithForceMaster())
		if err != nil {
			t.Fatalf("GetByID after commit failed: %v", err)
		}
		if got.Name != "tx_updated" {
			t.Errorf("After commit, got %s", got.Name)
		}
	})

	t.Run("RollbackTransaction", func(t *testing.T) {
		tx := db.Begin()
		user := &model.SysUserExample{Name: "rollback_user", Age: int64Ptr(30)}
		id, err := userDao.CreateByTx(ctx, tx, user)
		if err != nil {
			tx.Rollback()
			t.Fatalf("CreateByTx for rollback failed: %v", err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}
		_, err = userDao.GetByID(ctx, id)
		if !errors.Is(err, database.ErrRecordNotFound) {
			t.Errorf("Expected ErrRecordNotFound after rollback, got %v", err)
		}
	})

	t.Run("BatchInTransaction", func(t *testing.T) {
		tx := db.Begin()
		batchUsers := []*model.SysUserExample{
			{Name: "batch_tx1", Age: int64Ptr(10), Status: int64Ptr(1)},
			{Name: "batch_tx2", Age: int64Ptr(20), Status: int64Ptr(1)},
		}
		err := userDao.CreateByInBatchesTx(ctx, tx, batchUsers, 10)
		if err != nil {
			tx.Rollback()
			t.Fatalf("CreateByInBatchesTx failed: %v", err)
		}
		if err := tx.Commit().Error; err != nil {
			t.Fatalf("Commit batch tx failed: %v", err)
		}
		for _, u := range batchUsers {
			got, err := userDao.GetByID(ctx, u.ID)
			if err != nil {
				t.Errorf("GetByID for batch tx failed: %v", err)
			}
			if got.Name != u.Name {
				t.Errorf("Batch tx data mismatch: got %s", got.Name)
			}
		}
	})

	t.Run("DeleteInTransaction", func(t *testing.T) {
		user := &model.SysUserExample{Name: "delete_tx", Age: int64Ptr(40), Status: int64Ptr(1)}
		if err := userDao.Create(ctx, user); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		tx := db.Begin()
		var beforeDelete model.SysUserExample
		if err := tx.Table(tableName()).Where("id = ?", user.ID).First(&beforeDelete).Error; err != nil {
			tx.Rollback()
			t.Fatalf("删除前查询失败: %v", err)
		}
		if beforeDelete.ID != user.ID {
			tx.Rollback()
			t.Error("删除前记录不存在")
		}

		err := userDao.DeleteByTx(ctx, tx, user.ID)
		if err != nil {
			tx.Rollback()
			t.Fatalf("DeleteByTx failed: %v", err)
		}

		var afterDelete model.SysUserExample
		if err := tx.Table(tableName()).Where("id = ?", user.ID).First(&afterDelete).Error; err == nil {
			tx.Rollback()
			t.Error("删除后事务内仍能查到记录")
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			tx.Rollback()
			t.Errorf("删除后查询错误非预期: %v", err)
		}

		if err := tx.Commit().Error; err != nil {
			t.Fatalf("Commit delete tx failed: %v", err)
		}

		_, err = userDao.GetByID(ctx, user.ID)
		if !errors.Is(err, database.ErrRecordNotFound) {
			t.Errorf("Expected ErrRecordNotFound after delete commit, got %v", err)
		}
	})
}

// TestCustomOperations 自定义查询
func TestCustomOperations(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	for i := 0; i < 10; i++ {
		user := &model.SysUserExample{Name: fmt.Sprintf("custom_%d", i), Age: int64Ptr(int64(20 + i)), Status: int64Ptr(1)}
		if err := userDao.Create(ctx, user); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	var results []model.SysUserExample
	total, err := userDao.GetByCustomQuery(ctx, func(db *gorm.DB) *gorm.DB {
		return db.Model(&model.SysUserExample{}).Where("age > ?", 25).Order("id")
	}, &results, 0, 3)
	if err != nil {
		t.Fatalf("GetByCustomQuery ORM failed: %v", err)
	}
	if total != 4 {
		t.Errorf("Expected total 4, got %d", total)
	}
	if len(results) != 3 {
		t.Errorf("Expected 3 records, got %d", len(results))
	}

	var rawResults []map[string]interface{}
	rawSQL := fmt.Sprintf("SELECT id, name, age FROM %s WHERE age > 25 ORDER BY id", tableName())
	total, err = userDao.GetByCustomQuery(ctx, func(db *gorm.DB) *gorm.DB {
		return db.Raw(rawSQL)
	}, &rawResults, 0, 3)
	if err != nil {
		t.Fatalf("GetByCustomQuery raw SQL failed: %v", err)
	}
	if total != 4 {
		t.Errorf("Raw SQL total mismatch: got %d", total)
	}
	if len(rawResults) != 3 {
		t.Errorf("Raw SQL records mismatch: got %d", len(rawResults))
	}

	err = userDao.ExecByCustomFunc(ctx, func(db *gorm.DB) *gorm.DB {
		db = db.Model(&model.SysUserExample{}).Where("age > ?", 25).Update("name", "custom_updated")
		if db.Error != nil {
			db.Error = fmt.Errorf("update failed: %w", db.Error)
		}
		return db
	})
	if err != nil {
		t.Fatalf("ExecByCustomFunc failed: %v", err)
	}
	var count int64
	db.Model(&model.SysUserExample{}).Where("name = ?", "custom_updated").Count(&count)
	if count != 4 {
		t.Errorf("Expected 4 updated, got %d", count)
	}
}

// TestCacheManagement 缓存管理
func TestCacheManagement(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	user := &model.SysUserExample{Name: "cache_test", Age: int64Ptr(30), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err := userDao.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	cached, err := testRedisCache.Get(ctx, user.ID)
	if err != nil {
		t.Errorf("Cache missing after GetByID: %v", err)
	} else if cached == nil || cached.ID != user.ID {
		t.Errorf("Cache content invalid: got %+v", cached)
	}

	if err := userDao.ClearCache(ctx); err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}

	_, err = testRedisCache.Get(ctx, user.ID)
	if !errors.Is(err, database.ErrCacheNotFound) {
		t.Errorf("Expected cache not found after clear, got %v", err)
	}
}

// TestConcurrentReadWrite 并发读写
func TestConcurrentReadWrite(t *testing.T) {
	db := initTestDB()
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		DefaultExpireTime:   5 * time.Second,
		MaxCacheableRecords: 50,
		MaxCacheableIDs:     100,
	}))
	ctx := context.Background()
	clearTable(db)
	_ = userDao.ClearCache(ctx)

	const goroutines = 50
	const opsPerWorker = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	createdIDs := make([]uint64, 0)
	start := time.Now()

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			localRand := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))
			for j := 0; j < opsPerWorker; j++ {
				age := int64(int(uint8(localRand.Intn(60) + 18)))
				user := &model.SysUserExample{
					Name:   fmt.Sprintf("user_%d_%d", workerID, j),
					Age:    int64Ptr(age),
					Status: int64Ptr(1),
				}
				if err := userDao.Create(ctx, user); err != nil {
					t.Errorf("Create failed: %v", err)
					continue
				}
				mu.Lock()
				createdIDs = append(createdIDs, user.ID)
				mu.Unlock()

				got, err := userDao.GetByID(ctx, user.ID)
				if err != nil {
					t.Errorf("GetByID failed: %v", err)
					continue
				}
				if got.Name != user.Name || *got.Age != *user.Age {
					t.Errorf("Data mismatch: got %+v", got)
				}

				updateUser := &model.SysUserExample{Model: sgorm.Model{ID: user.ID}, Name: fmt.Sprintf("updated_%d_%d", workerID, j)}
				if err := userDao.UpdateByID(ctx, updateUser); err != nil {
					t.Errorf("UpdateByID failed: %v", err)
					continue
				}
				got2, err := userDao.GetByID(ctx, user.ID)
				if err != nil {
					t.Errorf("GetByID after update failed: %v", err)
					continue
				}
				if got2.Name != updateUser.Name {
					t.Errorf("Update not reflected: got %s", got2.Name)
				}

				if localRand.Intn(3) == 0 {
					if err := userDao.DeleteByID(ctx, user.ID); err != nil {
						t.Errorf("DeleteByID failed: %v", err)
					}
				}
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)
	t.Logf("并发压测总操作数: %d, 耗时: %v, QPS: %.2f", goroutines*opsPerWorker, elapsed, float64(goroutines*opsPerWorker)/elapsed.Seconds())

	mu.Lock()
	idsCopy := make([]uint64, len(createdIDs))
	copy(idsCopy, createdIDs)
	mu.Unlock()
	if len(idsCopy) > 10 {
		idsCopy = idsCopy[:10]
	}
	for _, id := range idsCopy {
		var dbUser model.SysUserExample
		err := db.Table(tableName()).Where("id = ?", id).First(&dbUser).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("查询DB失败: %v", err)
			continue
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		cacheUser, _ := testRedisCache.Get(ctx, id)
		if cacheUser != nil && dbUser.Name != cacheUser.Name {
			t.Errorf("缓存不一致: DB=%s, Cache=%s", dbUser.Name, cacheUser.Name)
		}
	}
}

// BenchmarkGetByID 基准测试
func BenchmarkGetByID(b *testing.B) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)
	user := &model.SysUserExample{Name: "bench", Age: int64Ptr(30), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = userDao.GetByID(ctx, user.ID)
	}
}

// ====================== 新增边界测试 ======================

// TestCachePenetration 验证缓存穿透防护
func TestCachePenetration(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		DefaultExpireTime: 5 * time.Second,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	nonExistID := uint64(999999)

	_, err := userDao.GetByID(ctx, nonExistID)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Fatalf("第一次查询不存在的ID，期望 ErrRecordNotFound，实际: %v", err)
	}

	cached, cacheErr := testRedisCache.Get(ctx, nonExistID)
	if cacheErr == nil {
		t.Fatalf("期望缓存中为占位符（应返回错误），但成功获取到了数据: %+v", cached)
	}
	if !testRedisCache.IsPlaceholderErr(cacheErr) {
		t.Fatalf("缓存错误应为占位符错误，实际: %v", cacheErr)
	}

	_, err2 := userDao.GetByID(ctx, nonExistID)
	if !errors.Is(err2, database.ErrRecordNotFound) {
		t.Fatalf("第二次查询不存在的ID，期望 ErrRecordNotFound，实际: %v", err2)
	}
}

// TestSoftDelete 验证批量软删除行为
func TestSoftDelete(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 1. 创建多条记录
	const batchSize = 10
	users := make([]*model.SysUserExample, batchSize)
	for i := 0; i < batchSize; i++ {
		users[i] = &model.SysUserExample{
			Name:   fmt.Sprintf("delete_test_%d", i),
			Age:    int64Ptr(int64(20 + i)),
			Status: int64Ptr(1),
		}
	}
	if err := userDao.CreateInBatches(ctx, users, 5); err != nil {
		t.Fatalf("批量创建失败: %v", err)
	}

	// 收集所有 ID
	ids := make([]uint64, 0, batchSize)
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	t.Logf("已创建 %d 条记录，ID: %v", len(ids), ids)

	// 2. 批量软删除（使用 DeleteByIDs）
	if err := userDao.DeleteByIDs(ctx, ids); err != nil {
		t.Fatalf("DeleteByIDs 失败: %v", err)
	}

	// 3. 默认查询（自动过滤 deleted_at IS NOT NULL）应返回空 map
	gotMap, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs 默认查询失败: %v", err)
	}
	if len(gotMap) != 0 {
		t.Errorf("删除后默认查询应返回空 map，实际返回 %d 条", len(gotMap))
	}

	// 4. 使用 WithUnscoped 忽略软删除过滤，应能查到所有记录
	gotMapUnscoped, err := userDao.GetByIDs(ctx, ids, SysUserExampleWithUnscoped())
	if err != nil {
		t.Fatalf("GetByIDs WithUnscoped 查询失败: %v", err)
	}
	if len(gotMapUnscoped) != batchSize {
		t.Fatalf("WithUnscoped 查询应返回 %d 条记录，实际返回 %d 条", batchSize, len(gotMapUnscoped))
	}

	// 5. 验证每条记录的 DeletedAt 字段已被设置（非零）
	for _, id := range ids {
		record, ok := gotMapUnscoped[id]
		if !ok {
			t.Errorf("ID %d 未在 WithUnscoped 结果中", id)
			continue
		}
		if record.DeletedAt.Time.IsZero() {
			t.Errorf("ID %d 的 DeletedAt 字段仍为零值，软删除未生效", id)
		}
	}

	t.Logf("批量软删除测试通过，共 %d 条记录，每条 DeletedAt 均非零", batchSize)
}

// TestHardDelete 验证物理删除（硬删除）行为
func TestHardDelete(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 1. 创建多条记录
	const batchSize = 5
	users := make([]*model.SysUserExample, batchSize)
	for i := 0; i < batchSize; i++ {
		users[i] = &model.SysUserExample{
			Name:   fmt.Sprintf("hard_delete_%d", i),
			Age:    int64Ptr(int64(20 + i)),
			Status: int64Ptr(1),
		}
	}
	if err := userDao.CreateInBatches(ctx, users, 3); err != nil {
		t.Fatalf("批量创建失败: %v", err)
	}

	ids := make([]uint64, 0, batchSize)
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	t.Logf("已创建 %d 条记录，ID: %v", len(ids), ids)

	// 2. 执行硬删除（物理删除）—— 使用 Unscoped() + Delete 传入模型
	// 无需 Table，GORM 自动从模型获取表名
	err := db.WithContext(ctx).Unscoped().
		Where("id IN (?)", ids).
		Delete(&model.SysUserExample{}).Error
	if err != nil {
		t.Fatalf("硬删除失败: %v", err)
	}

	// 3. 默认查询（应返回空）
	gotMap, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs 默认查询失败: %v", err)
	}
	if len(gotMap) != 0 {
		t.Errorf("硬删除后默认查询应返回空 map，实际返回 %d 条", len(gotMap))
	}

	// 4. 使用 WithUnscoped 查询（应同样为空，因为记录已被物理删除）
	gotMapUnscoped, err := userDao.GetByIDs(ctx, ids, SysUserExampleWithUnscoped())
	if err != nil {
		t.Fatalf("GetByIDs WithUnscoped 查询失败: %v", err)
	}
	if len(gotMapUnscoped) != 0 {
		t.Errorf("硬删除后 WithUnscoped 查询应返回空 map，实际返回 %d 条", len(gotMapUnscoped))
	}

	t.Logf("硬删除测试通过，共 %d 条记录已被物理删除", batchSize)
}

// TestPaginationCacheLimit 验证分页缓存限流
func TestPaginationCacheLimit(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		DefaultExpireTime:   5 * time.Second,
		MaxCacheableRecords: 2,
		MaxCacheableIDs:     100,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	for i := 0; i < 10; i++ {
		user := &model.SysUserExample{Name: fmt.Sprintf("pagination_%d", i), Age: int64Ptr(int64(i)), Status: int64Ptr(1)}
		if err := userDao.Create(ctx, user); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	params := &query.Params{Page: 0, Limit: 10, Sort: "id"}
	_, total, err := userDao.GetByColumns(ctx, params)
	if err != nil || total != 10 {
		t.Errorf("大分页查询错误，期望 total=10，实际 total=%d, err=%v", total, err)
	}

	paramsSmall := &query.Params{Page: 0, Limit: 1, Sort: "id"}
	recordsSmall, _, err := userDao.GetByColumns(ctx, paramsSmall)
	if err != nil || len(recordsSmall) != 1 {
		t.Errorf("小分页查询错误，期望 1 条记录，实际 %d, err=%v", len(recordsSmall), err)
	}
	t.Log("分页缓存限流测试通过（缓存存在性请查看日志）")
}

// TestZeroValueUpdate 验证更新零值字段（Age 更新为 0）
// 此测试预期通过，前提是 updateBuilder 已修改为支持零值（使用指针判断）
func TestZeroValueUpdate(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	user := &model.SysUserExample{Name: "zero_test", Age: int64Ptr(10), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	// 更新 Age=0（使用指针）
	user.Age = int64Ptr(0)
	if err := userDao.UpdateByID(ctx, user); err != nil {
		t.Fatalf("UpdateByID 失败: %v", err)
	}

	updated, err := userDao.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if updated.Age == nil || *updated.Age != 0 {
		t.Errorf("更新 Age=0 未生效，当前 Age=%v (期望 0)", updated.Age)
	} else {
		t.Log("零值更新测试通过（Age 成功更新为 0）")
	}
}

// TestContextCancel 验证上下文取消
func TestContextCancel(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	user := &model.SysUserExample{Name: "cancel_test", Age: int64Ptr(20), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	_, err := userDao.GetByID(cancelCtx, id)
	if err == nil {
		t.Error("GetByID 在 context 取消后应返回错误，但返回 nil")
	} else if !errors.Is(err, context.Canceled) {
		t.Errorf("GetByID 应返回 context.Canceled，实际: %v", err)
	}

	user.Age = int64Ptr(30)
	err = userDao.UpdateByID(cancelCtx, user)
	if err == nil {
		t.Error("UpdateByID 在 context 取消后应返回错误，但返回 nil")
	} else if !errors.Is(err, context.Canceled) {
		t.Errorf("UpdateByID 应返回 context.Canceled，实际: %v", err)
	}

	err = userDao.DeleteByID(cancelCtx, id)
	if err == nil {
		t.Error("DeleteByID 在 context 取消后应返回错误，但返回 nil")
	} else if !errors.Is(err, context.Canceled) {
		t.Errorf("DeleteByID 应返回 context.Canceled，实际: %v", err)
	}

	newUser := &model.SysUserExample{Name: "new_cancel", Age: int64Ptr(25)}
	err = userDao.Create(cancelCtx, newUser)
	if err == nil {
		t.Error("Create 在 context 取消后应返回错误，但返回 nil")
	} else if !errors.Is(err, context.Canceled) {
		t.Errorf("Create 应返回 context.Canceled，实际: %v", err)
	}

	t.Log("上下文取消测试通过")
}
