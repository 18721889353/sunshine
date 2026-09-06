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

	_ "github.com/pingcap/tidb/pkg/parser/test_driver"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/pkg/common/dao"
	"github.com/18721889353/sunshine/pkg/common/dao/test/cache"
	"github.com/18721889353/sunshine/pkg/common/dao/test/model"
	"github.com/18721889353/sunshine/pkg/sgorm"
	"github.com/18721889353/sunshine/pkg/sgorm/query"
)

// ---------- 辅助函数 ----------
func int64Ptr(v int64) *int64 {
	return &v
}

// ---------- 测试配置 ----------
var (
	testDBHost        = "bj-cdb-3hacsolk.sql.tencentcdb.com"
	testDBPort        = "23163"
	testDBUser        = "tcbank"
	testDBPassword    = "tcbank@1234"
	testDBName        = "coupon_platform"
	testRedisHost     = "43.143.78.234:6379"
	testRedisPassword = "jianguo123"
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
	_ = db.AutoMigrate(&model.SysUserExample{})
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
	_ = db.Exec("DELETE FROM " + tableName())
}

// ================================
// 原有测试用例（保留并微调）
// ================================

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

// TestBatchOperations 批量操作（提升至1000条）
func TestBatchOperations(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		DefaultExpireTime: 5 * time.Second,
		MaxBatchSize:      100,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	const total = 1000
	users := make([]*model.SysUserExample, total)
	for i := 0; i < total; i++ {
		users[i] = &model.SysUserExample{
			Name:   fmt.Sprintf("batch_%d", i),
			Age:    int64Ptr(int64(i % 60)),
			Status: int64Ptr(1),
		}
	}
	err := userDao.CreateInBatches(ctx, users, 100)
	if err != nil {
		t.Fatalf("CreateInBatches failed: %v", err)
	}

	ids := make([]uint64, total)
	for i, u := range users {
		ids[i] = u.ID
	}

	respMap, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(respMap) != total {
		t.Errorf("Expected %d records, got %d", total, len(respMap))
	}
	for _, id := range ids {
		if _, ok := respMap[id]; !ok {
			t.Errorf("ID %d missing", id)
		}
	}

	// 更新前100条
	for i := 0; i < 100; i++ {
		users[i].Name = "updated_batch"
		err = userDao.UpdateByID(ctx, users[i])
		if err != nil {
			t.Errorf("UpdateByID failed for id %d: %v", users[i].ID, err)
		}
	}

	respMap2, err := userDao.GetByIDs(ctx, ids[:100])
	if err != nil {
		t.Fatalf("GetByIDs after update failed: %v", err)
	}
	for i := 0; i < 100; i++ {
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
	_ = db.Model(&model.SysUserExample{}).Where("name = ?", "custom_updated").Count(&count)
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

// TestConcurrentReadWrite 并发读写（1000次操作）
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

// ================================
// 原有边界测试（保留）
// ================================

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

	ids := make([]uint64, 0, batchSize)
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	t.Logf("已创建 %d 条记录，ID: %v", len(ids), ids)

	if err := userDao.DeleteByIDs(ctx, ids); err != nil {
		t.Fatalf("DeleteByIDs 失败: %v", err)
	}

	gotMap, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs 默认查询失败: %v", err)
	}
	if len(gotMap) != 0 {
		t.Errorf("删除后默认查询应返回空 map，实际返回 %d 条", len(gotMap))
	}

	gotMapUnscoped, err := userDao.GetByIDs(ctx, ids, SysUserExampleWithUnscoped())
	if err != nil {
		t.Fatalf("GetByIDs WithUnscoped 查询失败: %v", err)
	}
	if len(gotMapUnscoped) != batchSize {
		t.Fatalf("WithUnscoped 查询应返回 %d 条记录，实际返回 %d 条", batchSize, len(gotMapUnscoped))
	}

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

	err := db.WithContext(ctx).Unscoped().
		Where("id IN (?)", ids).
		Delete(&model.SysUserExample{}).Error
	if err != nil {
		t.Fatalf("硬删除失败: %v", err)
	}

	gotMap, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs 默认查询失败: %v", err)
	}
	if len(gotMap) != 0 {
		t.Errorf("硬删除后默认查询应返回空 map，实际返回 %d 条", len(gotMap))
	}

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
	t.Log("分页缓存限流测试通过")
}

// TestZeroValueUpdate 验证更新零值字段（Age 更新为 0）
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

// ================================
// 新增边界测试用例
// ================================

// TestGetByIDsEmpty 空ID列表查询
func TestGetByIDsEmpty(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	result, err := userDao.GetByIDs(ctx, []uint64{})
	if err != nil {
		t.Fatalf("GetByIDs with empty list failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Expected empty map, got %d entries", len(result))
	}
}

// TestGetByIDsDuplicates 重复ID查询
func TestGetByIDsDuplicates(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	user := &model.SysUserExample{Name: "dup_test", Age: int64Ptr(25), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	result, err := userDao.GetByIDs(ctx, []uint64{id, id})
	if err != nil {
		t.Fatalf("GetByIDs with duplicates failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(result))
	}
	if _, ok := result[id]; !ok {
		t.Errorf("ID %d not in result", id)
	}
}

// TestGetByIDsLargeBatch 超大ID列表（分批验证）
func TestGetByIDsLargeBatch(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		MaxBatchSize: 10,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	const total = 25
	users := make([]*model.SysUserExample, total)
	for i := 0; i < total; i++ {
		users[i] = &model.SysUserExample{
			Name:   fmt.Sprintf("large_batch_%d", i),
			Age:    int64Ptr(int64(i)),
			Status: int64Ptr(1),
		}
	}
	if err := userDao.CreateInBatches(ctx, users, 5); err != nil {
		t.Fatalf("批量创建失败: %v", err)
	}

	ids := make([]uint64, total)
	for i, u := range users {
		ids[i] = u.ID
	}

	result, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs large batch failed: %v", err)
	}
	if len(result) != total {
		t.Errorf("Expected %d records, got %d", total, len(result))
	}
	for _, id := range ids {
		if _, ok := result[id]; !ok {
			t.Errorf("ID %d missing", id)
		}
	}
}

// TestUpdateAllZero 所有字段为零值更新（应报错）
func TestUpdateAllZero(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	user := &model.SysUserExample{Name: "zero_update", Age: int64Ptr(20), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	emptyUser := &model.SysUserExample{Model: sgorm.Model{ID: id}}
	err := userDao.UpdateByID(ctx, emptyUser)
	if err == nil {
		t.Error("Expected error for zero fields, got nil")
	} else if err.Error() != "no fields to update" {
		t.Errorf("Expected 'no fields to update', got %v", err)
	}
}

// TestDeleteNonExist 删除不存在的ID
func TestDeleteNonExist(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	err := userDao.DeleteByID(ctx, 99999999)
	if err != nil {
		t.Errorf("DeleteByID should not error when record not exist, got %v", err)
	}
}

// TestUnscopedGetByID 验证软删除后WithUnscoped查询
func TestUnscopedGetByID(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	user := &model.SysUserExample{Name: "unscoped_test", Age: int64Ptr(30), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	if err := userDao.DeleteByID(ctx, id); err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	_, err := userDao.GetByID(ctx, id)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("Expected ErrRecordNotFound, got %v", err)
	}

	got, err := userDao.GetByID(ctx, id, SysUserExampleWithUnscoped())
	if err != nil {
		t.Fatalf("GetByID WithUnscoped failed: %v", err)
	}
	if got.ID != id {
		t.Errorf("Expected ID %d, got %d", id, got.ID)
	}
	if got.DeletedAt.Time.IsZero() {
		t.Error("DeletedAt should be non-zero for soft-deleted record")
	}
}

// TestPlaceholderExpire 占位符过期后重新查询
func TestPlaceholderExpire(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		DefaultNotFoundExpireTime: 1 * time.Second,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	nonExistID := uint64(888888)

	_, err := userDao.GetByID(ctx, nonExistID)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Fatalf("第一次查询失败: %v", err)
	}

	_, err = userDao.GetByID(ctx, nonExistID)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("第二次查询应返回 NotFound，实际: %v", err)
	}

	time.Sleep(2 * time.Second)

	_, err = userDao.GetByID(ctx, nonExistID)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("过期后查询应返回 NotFound，实际: %v", err)
	}
}

// TestContextTimeout 上下文超时
func TestContextTimeout(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	user := &model.SysUserExample{Name: "timeout_test", Age: int64Ptr(20), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	_, err := userDao.GetByID(timeoutCtx, id)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	} else if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("Expected DeadlineExceeded or Canceled, got %v", err)
	}
}

// ================================
// 大数据量测试（10000条记录）
// ================================

// TestLargeDataset 测试大规模数据下的系统行为
// 默认跳过（因耗时较长），通过设置环境变量 TEST_LARGE=true 启用
// TestLargeDataset 测试大规模数据下的系统行为
// 默认跳过（因耗时较长），通过设置环境变量 TEST_LARGE=true 启用
func TestLargeDataset(t *testing.T) {

	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, SysUserExampleWithCacheConfig(dao.CacheConfig{
		DefaultExpireTime:   10 * time.Second,
		MaxCacheableRecords: 200,
		MaxCacheableIDs:     5000,
		MaxBatchSize:        200,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	const total = 10000
	t.Logf("开始创建 %d 条记录...", total)
	start := time.Now()

	// 分批创建
	batchSize := 500
	users := make([]*model.SysUserExample, 0, total)
	for i := 0; i < total; i++ {
		users = append(users, &model.SysUserExample{
			Name:   fmt.Sprintf("large_%d", i),
			Age:    int64Ptr(int64(i % 80)),
			Status: int64Ptr(int64(i%3 + 1)),
		})
		if len(users) == batchSize {
			if err := userDao.CreateInBatches(ctx, users, 100); err != nil {
				t.Fatalf("批量创建失败: %v", err)
			}
			users = users[:0]
		}
	}
	if len(users) > 0 {
		if err := userDao.CreateInBatches(ctx, users, 100); err != nil {
			t.Fatalf("批量创建失败: %v", err)
		}
	}
	t.Logf("创建完成，耗时: %v", time.Since(start))

	// 验证总数
	count, err := userDao.CountByCondition(ctx, &query.Conditions{})
	if err != nil {
		t.Fatalf("CountByCondition失败: %v", err)
	}
	if count != total {
		t.Fatalf("期望总数 %d，实际 %d", total, count)
	}

	// 批量查询所有ID（分页获取）
	allIDs := make([]uint64, 0, total)
	page := 0
	limit := 500
	for {
		params := &query.Params{Page: page, Limit: limit, Sort: "id"}
		records, _, err := userDao.GetByColumns(ctx, params)
		if err != nil {
			t.Fatalf("分页查询失败: %v", err)
		}
		if len(records) == 0 {
			break
		}
		for _, r := range records {
			allIDs = append(allIDs, r.ID)
		}
		if len(records) < limit {
			break
		}
		page++
	}
	if len(allIDs) != total {
		t.Fatalf("分页获取ID总数不匹配: 期望 %d，实际 %d", total, len(allIDs))
	}
	t.Logf("分页获取所有ID完成，共 %d 个", len(allIDs))

	// 随机取1000个ID进行批量查询
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(allIDs), func(i, j int) { allIDs[i], allIDs[j] = allIDs[j], allIDs[i] })
	sampleIDs := allIDs[:1000]

	start = time.Now()
	resultMap, err := userDao.GetByIDs(ctx, sampleIDs)
	if err != nil {
		t.Fatalf("GetByIDs 失败: %v", err)
	}
	if len(resultMap) != 1000 {
		t.Errorf("期望 1000 条结果，实际 %d", len(resultMap))
	}
	t.Logf("批量查询 1000 条记录耗时: %v", time.Since(start))

	// 条件查询：按 status=2 分页
	cond := &query.Conditions{Columns: []query.Column{{Name: "status", Value: "2"}}}
	count2, err := userDao.CountByCondition(ctx, cond)
	if err != nil {
		t.Fatalf("CountByCondition 失败: %v", err)
	}
	expected := total / 3 // status 为 1,2,3 均匀分布
	if count2 < int64(float64(expected)*0.9) || count2 > int64(float64(expected)*1.1) {
		t.Errorf("status=2 的记录数异常: 期望 ~%d，实际 %d", expected, count2)
	}

	params2 := &query.Params{Page: 0, Limit: 100, Sort: "-id", Columns: cond.Columns}
	records2, total2, err := userDao.GetByColumns(ctx, params2)
	if err != nil {
		t.Fatalf("GetByColumns 条件查询失败: %v", err)
	}
	if int64(len(records2)) != 100 || total2 != count2 {
		t.Errorf("分页数据不匹配: len=%d, total=%d, 期望 total=%d", len(records2), total2, count2)
	}

	// 更新所有 status=1 的记录（将 status 改为 3）
	updateEntity := &model.SysUserExample{Status: int64Ptr(3)}
	err = userDao.UpdateByCondition(ctx, &query.Conditions{Columns: []query.Column{{Name: "status", Value: "1"}}}, updateEntity)
	if err != nil {
		t.Fatalf("UpdateByCondition 失败: %v", err)
	}
	newCount, _ := userDao.CountByCondition(ctx, &query.Conditions{Columns: []query.Column{{Name: "status", Value: "3"}}})
	if newCount < 3000 {
		t.Errorf("更新后 status=3 的记录数异常: %d", newCount)
	}

	// 删除 status=2 的所有记录（软删除）
	err = userDao.DeleteByCondition(ctx, &query.Conditions{Columns: []query.Column{{Name: "status", Value: "2"}}})
	if err != nil {
		t.Fatalf("DeleteByCondition 失败: %v", err)
	}
	remainCount, _ := userDao.CountByCondition(ctx, &query.Conditions{})
	expectedRemain := total - int(count2)
	if remainCount != int64(expectedRemain) {
		t.Errorf("删除后剩余记录数错误: 期望 %d，实际 %d", expectedRemain, remainCount)
	}

	// 验证软删除记录可用 WithUnscoped 查到
	deletedIDs := make([]uint64, 0, 100)
	page2 := 0
	for {
		params3 := &query.Params{Page: page2, Limit: 100, Sort: "id", Columns: []query.Column{{Name: "status", Value: "2"}}}
		records3, _, err := userDao.GetByColumns(ctx, params3, SysUserExampleWithUnscoped())
		if err != nil {
			t.Fatalf("查询已删除记录失败: %v", err)
		}
		if len(records3) == 0 {
			break
		}
		for _, r := range records3 {
			deletedIDs = append(deletedIDs, r.ID)
		}
		page2++
	}
	if len(deletedIDs) != int(count2) {
		t.Errorf("WithUnscoped 获取已删除记录数不匹配: 期望 %d，实际 %d", count2, len(deletedIDs))
	}

	t.Logf("大数据量测试完成，总耗时: %v", time.Since(start))
}

// ================================
// 性能基准测试
// ================================

// BenchmarkGetByID 单条查询基准
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

// BenchmarkGetByIDs 批量查询基准
func BenchmarkGetByIDs(b *testing.B) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	const size = 100
	users := make([]*model.SysUserExample, size)
	for i := 0; i < size; i++ {
		users[i] = &model.SysUserExample{
			Name:   fmt.Sprintf("bench_batch_%d", i),
			Age:    int64Ptr(int64(i % 50)),
			Status: int64Ptr(1),
		}
	}
	if err := userDao.CreateInBatches(ctx, users, 20); err != nil {
		b.Fatalf("CreateInBatches failed: %v", err)
	}
	ids := make([]uint64, size)
	for i, u := range users {
		ids[i] = u.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = userDao.GetByIDs(ctx, ids)
	}
}

// BenchmarkGetByColumns 分页查询基准
func BenchmarkGetByColumns(b *testing.B) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	for i := 0; i < 50; i++ {
		user := &model.SysUserExample{
			Name:   fmt.Sprintf("bench_col_%d", i),
			Age:    int64Ptr(int64(i % 30)),
			Status: int64Ptr(1),
		}
		if err := userDao.Create(ctx, user); err != nil {
			b.Fatalf("Create failed: %v", err)
		}
	}

	params := &query.Params{Page: 0, Limit: 20, Sort: "-id"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = userDao.GetByColumns(ctx, params)
	}
}
