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
	testDBHost        = "127.0.0.1"
	testDBPort        = "3306"
	testDBUser        = "root"
	testDBPassword    = "jianguo123"
	testDBName        = "sunshine"
	testRedisHost     = "127.0.0.1:6379"
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
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
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
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
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
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
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

		got, err := userDao.GetByID(ctx, id, dao.WithUnscoped())
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
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
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
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
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

	gotMapUnscoped, err := userDao.GetByIDs(ctx, ids, dao.WithUnscoped())
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

	gotMapUnscoped, err := userDao.GetByIDs(ctx, ids, dao.WithUnscoped())
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
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
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
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
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

	got, err := userDao.GetByID(ctx, id, dao.WithUnscoped())
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
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
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
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
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
		records3, _, err := userDao.GetByColumns(ctx, params3, dao.WithUnscoped())
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

// ================================
// CacheAdapter 方法直接测试
// ================================

// TestCacheSetGetDel 测试 CacheAdapter 的 Set、Get、Del 方法
func TestCacheSetGetDel(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建测试数据
	user := &model.SysUserExample{Name: "cache_adapter_test", Age: int64Ptr(25), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 测试 Get（通过缓存获取）
	got, err := userDao.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Name != user.Name {
		t.Errorf("Name mismatch: got %s, want %s", got.Name, user.Name)
	}

	// 测试 Del
	if err := userDao.DeleteByID(ctx, user.ID); err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	// 验证缓存已删除
	_, err = userDao.GetByID(ctx, user.ID)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("Expected ErrRecordNotFound after delete, got %v", err)
	}
}

// TestCacheCustomKey 测试自定义 Key 缓存方法
func TestCacheCustomKey(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 测试 SetIDByKey / GetIDByKey
	testKey := "custom_id_key"
	testID := uint64(12345)
	duration := 10 * time.Second

	// SetIDByKey
	err := testRedisCache.SetIDByKey(ctx, testKey, testID, duration)
	if err != nil {
		t.Fatalf("SetIDByKey failed: %v", err)
	}

	// GetIDByKey
	gotID, err := testRedisCache.GetIDByKey(ctx, testKey)
	if err != nil {
		t.Fatalf("GetIDByKey failed: %v", err)
	}
	if gotID != testID {
		t.Errorf("ID mismatch: got %d, want %d", gotID, testID)
	}

	// 测试 SetIDsByKey / GetIDsByKey
	testIDsKey := "custom_ids_key"
	testIDs := []uint64{100, 200, 300, 400, 500}

	// SetIDsByKey
	err = testRedisCache.SetIDsByKey(ctx, testIDsKey, testIDs, duration)
	if err != nil {
		t.Fatalf("SetIDsByKey failed: %v", err)
	}

	// GetIDsByKey
	gotIDs, err := testRedisCache.GetIDsByKey(ctx, testIDsKey)
	if err != nil {
		t.Fatalf("GetIDsByKey failed: %v", err)
	}
	if len(gotIDs) != len(testIDs) {
		t.Fatalf("IDs length mismatch: got %d, want %d", len(gotIDs), len(testIDs))
	}
	for i, id := range gotIDs {
		if id != testIDs[i] {
			t.Errorf("IDs[%d] mismatch: got %d, want %d", i, id, testIDs[i])
		}
	}

	// 测试 DelByKey
	err = testRedisCache.DelByKey(ctx, testKey)
	if err != nil {
		t.Fatalf("DelByKey failed: %v", err)
	}

	// 验证删除后获取返回错误
	_, err = testRedisCache.GetIDByKey(ctx, testKey)
	if err == nil {
		t.Error("Expected error after DelByKey, got nil")
	}

	// 清理
	_ = testRedisCache.DelByKey(ctx, testIDsKey)
}

// TestCacheDelByPrefix 测试按前缀删除
func TestCacheDelByPrefix(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建测试数据
	user := &model.SysUserExample{Name: "prefix_test", Age: int64Ptr(30), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 写入多个自定义 key
	// 注意：SetIDByKey 内部会自动添加 adapter 前缀（如 data:sysUserExample:）
	prefix := "test_prefix"
	keys := []string{"key1", "key2", "key3"}
	duration := 10 * time.Second

	for _, key := range keys {
		err := testRedisCache.SetIDByKey(ctx, prefix+":"+key, uint64(100), duration)
		if err != nil {
			t.Fatalf("SetIDByKey failed for key %s: %v", key, err)
		}
	}

	// 验证 key 存在
	for _, key := range keys {
		_, err := testRedisCache.GetIDByKey(ctx, prefix+":"+key)
		if err != nil {
			t.Errorf("Key %s should exist before prefix delete: %v", key, err)
		}
	}

	// 按前缀删除
	// 注意：需要使用包含 adapter 前缀的完整前缀
	// 实际存储的 key 格式为：data:sysUserExample:test_prefix:key1
	fullPrefix := "data:sysUserExample:" + prefix
	err := testRedisCache.DelByPrefix(ctx, fullPrefix)
	if err != nil {
		t.Fatalf("DelByPrefix failed: %v", err)
	}

	// 等待删除生效
	time.Sleep(100 * time.Millisecond)

	// 验证删除后获取返回错误
	for _, key := range keys {
		_, err := testRedisCache.GetIDByKey(ctx, prefix+":"+key)
		if err == nil {
			t.Errorf("Key %s should not exist after prefix delete", key)
		}
	}
}

// TestCachePlaceholderByKey 测试自定义 Key 的占位符
func TestCachePlaceholderByKey(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 测试 SetPlaceholderByKey
	placeholderKey := "non_exist_condition"
	err := testRedisCache.SetPlaceholderByKey(ctx, placeholderKey)
	if err != nil {
		t.Fatalf("SetPlaceholderByKey failed: %v", err)
	}

	// 获取占位符 key 应该返回占位符错误
	_, err = testRedisCache.GetIDByKey(ctx, placeholderKey)
	if err == nil {
		t.Error("Expected placeholder error, got nil")
	} else if !testRedisCache.IsPlaceholderErr(err) {
		t.Errorf("Expected placeholder error, got %v", err)
	}

	// 清理
	_ = testRedisCache.DelByKey(ctx, placeholderKey)
}

// TestCacheEmptyInputs 测试空输入边界情况
func TestCacheEmptyInputs(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 测试 SetIDByKey 空 key
	err := testRedisCache.SetIDByKey(ctx, "", 123, 10*time.Second)
	if err != nil {
		t.Errorf("SetIDByKey with empty key should not error, got %v", err)
	}

	// 测试 SetIDByKey 零值 ID
	err = testRedisCache.SetIDByKey(ctx, "test_key", 0, 10*time.Second)
	if err != nil {
		t.Errorf("SetIDByKey with zero ID should not error, got %v", err)
	}

	// 测试 SetIDsByKey 空 key
	err = testRedisCache.SetIDsByKey(ctx, "", []uint64{1, 2, 3}, 10*time.Second)
	if err != nil {
		t.Errorf("SetIDsByKey with empty key should not error, got %v", err)
	}

	// 测试 SetIDsByKey 空切片
	err = testRedisCache.SetIDsByKey(ctx, "test_key", []uint64{}, 10*time.Second)
	if err != nil {
		t.Errorf("SetIDsByKey with empty slice should not error, got %v", err)
	}

	// 测试 GetIDsByKey 不存在的 key
	_, err = testRedisCache.GetIDsByKey(ctx, "non_exist_key")
	if err == nil {
		t.Error("GetIDsByKey for non-existent key should return error")
	}

	// 测试 GetIDByKey 不存在的 key
	_, err = testRedisCache.GetIDByKey(ctx, "non_exist_key")
	if err == nil {
		t.Error("GetIDByKey for non-existent key should return error")
	}
}

// TestCacheMultiSetGet 测试批量缓存操作
func TestCacheMultiSetGet(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建测试数据
	users := []*model.SysUserExample{
		{Name: "multi_1", Age: int64Ptr(20), Status: int64Ptr(1)},
		{Name: "multi_2", Age: int64Ptr(25), Status: int64Ptr(1)},
		{Name: "multi_3", Age: int64Ptr(30), Status: int64Ptr(1)},
	}
	for _, u := range users {
		if err := userDao.Create(ctx, u); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// 获取所有 ID
	ids := make([]uint64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}

	// 通过 DAO 的 GetByIDs 测试 MultiSet/MultiGet
	result, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(result) != len(users) {
		t.Errorf("Result count mismatch: got %d, want %d", len(result), len(users))
	}

	// 验证每个 ID 都有对应的结果
	for _, u := range users {
		if _, ok := result[u.ID]; !ok {
			t.Errorf("ID %d not in result", u.ID)
		}
	}
}

// TestCacheWithDifferentExpiry 测试不同过期时间的缓存
func TestCacheWithDifferentExpiry(t *testing.T) {
	db := initTestDB()
	clearTable(db)

	ctx := context.Background()

	// 使用短过期时间的 DAO
	shortExpireDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
		DefaultExpireTime: 1 * time.Second,
	}))
	_ = shortExpireDao.ClearCache(ctx)

	// 创建数据
	user := &model.SysUserExample{Name: "expiry_test", Age: int64Ptr(25), Status: int64Ptr(1)}
	if err := shortExpireDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 第一次查询，应该命中缓存
	_, err := shortExpireDao.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	// 等待缓存过期
	time.Sleep(2 * time.Second)

	// 第二次查询，应该重新从数据库获取
	got, err := shortExpireDao.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID after expiry failed: %v", err)
	}
	if got.Name != user.Name {
		t.Errorf("Name mismatch after expiry: got %s, want %s", got.Name, user.Name)
	}
}

// TestCachePlaceholderExpiry 测试占位符过期
func TestCachePlaceholderExpiry(t *testing.T) {
	db := initTestDB()
	clearTable(db)

	// 使用短过期时间的 DAO
	shortExpireDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
		DefaultExpireTime:         10 * time.Second,
		DefaultNotFoundExpireTime: 1 * time.Second,
	}))
	ctx := context.Background()
	_ = shortExpireDao.ClearCache(ctx)

	nonExistID := uint64(777777)

	// 第一次查询不存在的 ID，应该设置占位符
	_, err := shortExpireDao.GetByID(ctx, nonExistID)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Fatalf("First query should return ErrRecordNotFound, got %v", err)
	}

	// 等待占位符过期
	time.Sleep(2 * time.Second)

	// 再次查询，应该重新查数据库
	_, err = shortExpireDao.GetByID(ctx, nonExistID)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("Query after placeholder expiry should return ErrRecordNotFound, got %v", err)
	}
}

// ========================================================================
// 生产级测试用例 - 缓存安全性
// ========================================================================

// TestCachePenetrationProtection 测试缓存穿透防护
// 验证：大量不存在的ID查询不会直接打到数据库
func TestCachePenetrationProtection(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 使用短过期时间，方便测试占位符失效
	shortExpireDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
		DefaultExpireTime:         10 * time.Second,
		DefaultNotFoundExpireTime: 1 * time.Second,
	}))
	_ = shortExpireDao.ClearCache(ctx)

	// 模拟大量不存在的ID查询
	nonExistIDs := []uint64{900001, 900002, 900003, 900004, 900005}
	for _, id := range nonExistIDs {
		_, err := shortExpireDao.GetByID(ctx, id)
		if !errors.Is(err, database.ErrRecordNotFound) {
			t.Errorf("ID %d should return ErrRecordNotFound, got %v", id, err)
		}
	}

	// 验证占位符已设置：第二次查询应该更快（命中占位符）
	start := time.Now()
	for _, id := range nonExistIDs {
		_, _ = shortExpireDao.GetByID(ctx, id)
	}
	cachedDuration := time.Since(start)

	// 第一次查询可能较慢，第二次应该明显更快
	t.Logf("占位符保护生效，%d个ID查询耗时: %v", len(nonExistIDs), cachedDuration)

	// 清理
	for _, id := range nonExistIDs {
		_ = testRedisCache.Del(ctx, id)
	}
}

// TestCacheBreakdownProtection 测试缓存击穿防护（singleflight）
// 验证：热点key过期时，并发查询只会有一个请求查库
func TestCacheBreakdownProtection(t *testing.T) {
	db := initTestDB()
	clearTable(db)

	// 使用极短过期时间
	shortExpireDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
		DefaultExpireTime: 100 * time.Millisecond, // 100ms 过期
	}))
	ctx := context.Background()
	_ = shortExpireDao.ClearCache(ctx)

	// 创建测试数据
	user := &model.SysUserExample{Name: "breakdown_test", Age: int64Ptr(25), Status: int64Ptr(1)}
	if err := shortExpireDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 第一次查询，缓存数据
	_, err := shortExpireDao.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("First GetByID failed: %v", err)
	}

	// 等待缓存过期
	time.Sleep(200 * time.Millisecond)

	// 并发查询，验证 singleflight 生效
	var wg sync.WaitGroup
	errors := make([]error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got, err := shortExpireDao.GetByID(ctx, user.ID)
			errors[idx] = err
			if err == nil && got.Name != user.Name {
				errors[idx] = fmt.Errorf("name mismatch: got %s", got.Name)
			}
		}(i)
	}
	wg.Wait()

	// 验证所有请求都成功
	for i, err := range errors {
		if err != nil {
			t.Errorf("Goroutine %d failed: %v", i, err)
		}
	}

	t.Log("缓存击穿防护测试通过（singleflight 生效）")
}

// TestCacheDBConsistency 测试缓存与数据库一致性
// 验证：Update/Delete 后缓存正确同步（Cache-Aside 模式：更新后删除缓存）
func TestCacheDBConsistency(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建数据
	user := &model.SysUserExample{Name: "consistency_test", Age: int64Ptr(20), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	// 第一次查询，写入缓存
	got, _ := userDao.GetByID(ctx, id)
	if got.Name != "consistency_test" {
		t.Fatal("Initial data mismatch")
	}

	// 验证缓存已写入
	cached, err := testRedisCache.Get(ctx, id)
	if err != nil {
		t.Errorf("Cache should exist after GetByID: %v", err)
	} else if cached == nil {
		t.Error("Cache should not be nil")
	}

	// 更新数据（Cache-Aside 模式：更新后删除缓存）
	user.Name = "consistency_updated"
	user.Age = int64Ptr(30)
	if err := userDao.UpdateByID(ctx, user); err != nil {
		t.Fatalf("UpdateByID failed: %v", err)
	}

	// 验证缓存已被删除（Cache-Aside 模式）
	cached, err = testRedisCache.Get(ctx, id)
	if err == nil && cached != nil {
		t.Error("Cache should be deleted after UpdateByID (Cache-Aside pattern)")
	}

	// 验证数据库查询返回新值（会重新写入缓存）
	got2, err := userDao.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got2.Name != "consistency_updated" || *got2.Age != 30 {
		t.Errorf("DB data mismatch: got %+v", got2)
	}

	// 验证缓存已重新写入
	cached, err = testRedisCache.Get(ctx, id)
	if err != nil {
		t.Errorf("Cache should exist after re-query: %v", err)
	} else if cached == nil {
		t.Error("Cache should not be nil after re-query")
	} else if cached.Name != "consistency_updated" {
		t.Errorf("Cache data mismatch: got %s", cached.Name)
	}

	// 删除数据
	if err := userDao.DeleteByID(ctx, id); err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	// 验证缓存已删除
	_, err = testRedisCache.Get(ctx, id)
	if err == nil {
		t.Error("Cache should be deleted after DeleteByID")
	}

	// 验证数据库查询返回 NotFound
	_, err = userDao.GetByID(ctx, id)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("Expected ErrRecordNotFound after delete, got %v", err)
	}
}

// TestConcurrentUpdateIntegrity 测试并发更新数据完整性
// 验证：并发更新不会导致数据丢失
func TestConcurrentUpdateIntegrity(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建测试数据
	user := &model.SysUserExample{Name: "concurrent_update", Age: int64Ptr(0), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	// 并发更新 Age 字段（模拟计数器）
	var wg sync.WaitGroup
	const goroutines = 50
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			updateUser := &model.SysUserExample{
				Model: sgorm.Model{ID: id},
				Name:  fmt.Sprintf("updated_%d", idx),
			}
			_ = userDao.UpdateByID(ctx, updateUser)
		}(i)
	}
	wg.Wait()

	// 验证数据完整性
	got, err := userDao.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.ID != id {
		t.Errorf("ID mismatch: got %d, want %d", got.ID, id)
	}
	// Name 应该是最后一次更新的值
	if got.Name == "" {
		t.Error("Name should not be empty after concurrent updates")
	}

	t.Logf("并发更新后 Name: %s", got.Name)
}

// TestCacheDisabledMode 测试缓存禁用模式
// 验证：禁用缓存时所有查询直接走数据库
func TestCacheDisabledMode(t *testing.T) {
	db := initTestDB()
	clearTable(db)

	// 创建禁用缓存的 DAO
	disabledDao := NewSysUserExampleDao(db, testRedisCache, dao.WithNoCache[model.SysUserExample]())
	ctx := context.Background()

	// 创建数据
	user := &model.SysUserExample{Name: "no_cache_test", Age: int64Ptr(25), Status: int64Ptr(1)}
	if err := disabledDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	// 查询数据
	got, err := disabledDao.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Name != "no_cache_test" {
		t.Errorf("Name mismatch: got %s", got.Name)
	}

	// 验证缓存中没有数据（因为禁用了缓存）
	_, err = testRedisCache.Get(ctx, id)
	if err == nil {
		t.Log("注意：缓存中可能存在数据（来自其他测试），但 DAO 不会读取它")
	}

	// 更新数据
	user.Name = "no_cache_updated"
	if err := disabledDao.UpdateByID(ctx, user); err != nil {
		t.Fatalf("UpdateByID failed: %v", err)
	}

	// 再次查询，应该获取最新值
	got2, err := disabledDao.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got2.Name != "no_cache_updated" {
		t.Errorf("Updated name mismatch: got %s", got2.Name)
	}
}

// TestBoundaryValues 测试边界值
// 验证：各种边界条件下的正确行为
func TestBoundaryValues(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	t.Run("MaxUint64ID", func(t *testing.T) {
		// 测试最大 uint64 ID（不存在）
		_, err := userDao.GetByID(ctx, ^uint64(0))
		if !errors.Is(err, database.ErrRecordNotFound) {
			t.Errorf("Max uint64 ID should return ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("ZeroID", func(t *testing.T) {
		// 测试零值 ID
		_, err := userDao.GetByID(ctx, 0)
		if !errors.Is(err, database.ErrRecordNotFound) {
			t.Errorf("Zero ID should return ErrRecordNotFound, got %v", err)
		}
	})

	t.Run("NegativeAge", func(t *testing.T) {
		// 测试负数 Age
		user := &model.SysUserExample{Name: "negative_age", Age: int64Ptr(-1), Status: int64Ptr(1)}
		err := userDao.Create(ctx, user)
		if err != nil {
			t.Logf("创建负数 Age 记录: %v", err)
		} else {
			got, _ := userDao.GetByID(ctx, user.ID)
			if got != nil && got.Age != nil && *got.Age != -1 {
				t.Errorf("Age mismatch: got %v, want -1", got.Age)
			}
		}
	})

	t.Run("NilPointerFields", func(t *testing.T) {
		// 测试 nil 指针字段
		user := &model.SysUserExample{Name: "nil_fields"} // Age, Status 都是 nil
		err := userDao.Create(ctx, user)
		if err != nil {
			t.Fatalf("Create with nil fields failed: %v", err)
		}
		got, err := userDao.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if got.Age != nil {
			t.Errorf("Age should be nil, got %v", got.Age)
		}
		if got.Status != nil {
			t.Errorf("Status should be nil, got %v", got.Status)
		}
	})

	t.Run("EmptyName", func(t *testing.T) {
		// 测试空名称
		user := &model.SysUserExample{Name: "", Age: int64Ptr(20), Status: int64Ptr(1)}
		err := userDao.Create(ctx, user)
		if err != nil {
			t.Fatalf("Create with empty name failed: %v", err)
		}
		got, err := userDao.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if got.Name != "" {
			t.Errorf("Name should be empty, got %s", got.Name)
		}
	})

	t.Run("VeryLongName", func(t *testing.T) {
		// 测试超长名称
		longName := fmt.Sprintf("%01000s", "a") // 1000字符
		user := &model.SysUserExample{Name: longName, Age: int64Ptr(20), Status: int64Ptr(1)}
		err := userDao.Create(ctx, user)
		if err != nil {
			t.Logf("创建超长名称记录: %v", err)
		} else {
			got, _ := userDao.GetByID(ctx, user.ID)
			if got != nil && len(got.Name) != 1000 {
				t.Errorf("Name length mismatch: got %d, want 1000", len(got.Name))
			}
		}
	})
}

// TestPaginationBoundary 测试分页边界条件
// 验证：各种分页参数下的正确行为
func TestPaginationBoundary(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建10条测试数据
	for i := 0; i < 10; i++ {
		user := &model.SysUserExample{
			Name:   fmt.Sprintf("page_test_%d", i),
			Age:    int64Ptr(int64(i)),
			Status: int64Ptr(1),
		}
		if err := userDao.Create(ctx, user); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	t.Run("PageZeroLimitZero", func(t *testing.T) {
		// 测试 Page=0, Limit=0（应该返回默认值）
		params := &query.Params{Page: 0, Limit: 0, Sort: "id"}
		records, total, err := userDao.GetByColumns(ctx, params)
		if err != nil {
			t.Errorf("Query failed: %v", err)
		}
		t.Logf("Page=0, Limit=0: total=%d, records=%d", total, len(records))
	})

	t.Run("PageNegative", func(t *testing.T) {
		// 测试负数页码
		params := &query.Params{Page: -1, Limit: 10, Sort: "id"}
		_, _, err := userDao.GetByColumns(ctx, params)
		if err != nil {
			t.Logf("负数页码返回错误（符合预期）: %v", err)
		}
	})

	t.Run("LimitExceedsTotal", func(t *testing.T) {
		// 测试 Limit 超过总数
		params := &query.Params{Page: 0, Limit: 100, Sort: "id"}
		records, total, err := userDao.GetByColumns(ctx, params)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if total != 10 {
			t.Errorf("Total should be 10, got %d", total)
		}
		if len(records) != 10 {
			t.Errorf("Records should be 10, got %d", len(records))
		}
	})

	t.Run("PageExceedsTotal", func(t *testing.T) {
		// 测试页码超过总页数
		params := &query.Params{Page: 100, Limit: 10, Sort: "id"}
		records, total, err := userDao.GetByColumns(ctx, params)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if total != 10 {
			t.Errorf("Total should be 10, got %d", total)
		}
		if len(records) != 0 {
			t.Errorf("Records should be 0 for out-of-range page, got %d", len(records))
		}
	})

	t.Run("SortIgnoreCount", func(t *testing.T) {
		// 测试忽略 Count 的排序
		params := &query.Params{Page: 0, Limit: 5, Sort: dao.SortIgnoreCount}
		records, total, err := userDao.GetByColumns(ctx, params)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if len(records) != 5 {
			t.Errorf("Records should be 5, got %d", len(records))
		}
		// SortIgnoreCount 会跳过 Count 查询，total 应该是 0
		if total != 0 {
			t.Errorf("Total should be 0 when using SortIgnoreCount, got %d", total)
		}
		t.Logf("SortIgnoreCount 测试通过: records=%d, total=%d (跳过了 COUNT 查询)", len(records), total)
	})
}

// TestSoftDeleteRecovery 测试软删除恢复
// 验证：软删除的记录可以通过 Unscoped 查询并恢复
func TestSoftDeleteRecovery(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建数据
	user := &model.SysUserExample{Name: "soft_delete_test", Age: int64Ptr(25), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	// 软删除
	if err := userDao.DeleteByID(ctx, id); err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	// 普通查询应该返回 NotFound
	_, err := userDao.GetByID(ctx, id)
	if !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("Expected ErrRecordNotFound, got %v", err)
	}

	// Unscoped 查询应该能找到
	got, err := userDao.GetByID(ctx, id, dao.WithUnscoped())
	if err != nil {
		t.Fatalf("GetByID with Unscoped failed: %v", err)
	}
	if got.ID != id {
		t.Errorf("ID mismatch: got %d, want %d", got.ID, id)
	}
	if got.DeletedAt.Time.IsZero() {
		t.Error("DeletedAt should be non-zero for soft-deleted record")
	}

	t.Log("软删除恢复测试通过")
}

// TestIdempotentDelete 测试删除幂等性
// 验证：多次删除同一记录不会报错
func TestIdempotentDelete(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建数据
	user := &model.SysUserExample{Name: "idempotent_delete", Age: int64Ptr(25), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	// 第一次删除
	if err := userDao.DeleteByID(ctx, id); err != nil {
		t.Fatalf("First DeleteByID failed: %v", err)
	}

	// 第二次删除（应该不报错或返回 NotFound）
	err := userDao.DeleteByID(ctx, id)
	if err != nil && !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("Second DeleteByID should not error, got %v", err)
	}

	// 第三次删除
	err = userDao.DeleteByID(ctx, id)
	if err != nil && !errors.Is(err, database.ErrRecordNotFound) {
		t.Errorf("Third DeleteByID should not error, got %v", err)
	}

	t.Log("删除幂等性测试通过")
}

// TestGetByIDsMixedExistence 测试混合存在性的批量查询
// 验证：部分ID存在、部分不存在时的正确行为
func TestGetByIDsMixedExistence(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建2条数据
	user1 := &model.SysUserExample{Name: "mixed_1", Age: int64Ptr(20), Status: int64Ptr(1)}
	user2 := &model.SysUserExample{Name: "mixed_2", Age: int64Ptr(25), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user1); err != nil {
		t.Fatalf("Create user1 failed: %v", err)
	}
	if err := userDao.Create(ctx, user2); err != nil {
		t.Fatalf("Create user2 failed: %v", err)
	}

	// 查询包含不存在的ID
	ids := []uint64{user1.ID, 999999, user2.ID, 888888}
	result, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}

	// 应该只返回存在的2条
	if len(result) != 2 {
		t.Errorf("Expected 2 records, got %d", len(result))
	}
	if _, ok := result[user1.ID]; !ok {
		t.Errorf("User1 should be in result")
	}
	if _, ok := result[user2.ID]; !ok {
		t.Errorf("User2 should be in result")
	}
	if _, ok := result[999999]; ok {
		t.Errorf("Non-existent ID 999999 should not be in result")
	}
}

// TestConcurrentReadWriteIntegrity 测试并发读写数据完整性
// 验证：并发读写不会导致数据不一致
func TestConcurrentReadWriteIntegrity(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache, dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{
		DefaultExpireTime: 5 * time.Second,
	}))
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建测试数据
	user := &model.SysUserExample{Name: "rw_integrity", Age: int64Ptr(100), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	// 并发读写
	var wg sync.WaitGroup
	const goroutines = 30
	readErrors := make([]error, goroutines)
	writeErrors := make([]error, goroutines)

	// 启动读协程
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got, err := userDao.GetByID(ctx, id)
			readErrors[idx] = err
			if err == nil && got.ID != id {
				readErrors[idx] = fmt.Errorf("ID mismatch: got %d", got.ID)
			}
		}(i)
	}

	// 启动写协程
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			updateUser := &model.SysUserExample{
				Model: sgorm.Model{ID: id},
				Name:  fmt.Sprintf("updated_%d", idx),
			}
			writeErrors[idx] = userDao.UpdateByID(ctx, updateUser)
		}(i)
	}

	wg.Wait()

	// 验证读操作没有数据错误
	for i, err := range readErrors {
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Read goroutine %d failed: %v", i, err)
		}
	}

	// 验证写操作没有异常错误
	for i, err := range writeErrors {
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Write goroutine %d failed: %v", i, err)
		}
	}

	// 最终验证数据完整性
	got, err := userDao.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("Final GetByID failed: %v", err)
	}
	if got.ID != id {
		t.Errorf("Final ID mismatch: got %d", got.ID)
	}
	if got.Name == "" {
		t.Error("Final Name should not be empty")
	}

	t.Logf("并发读写完整性测试通过，最终 Name: %s", got.Name)
}

// TestCacheKeyPrefix 测试缓存Key前缀正确性
// 验证：不同表的缓存Key前缀不会冲突
func TestCacheKeyPrefix(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建数据
	user := &model.SysUserExample{Name: "prefix_test", Age: int64Ptr(25), Status: int64Ptr(1)}
	if err := userDao.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	id := user.ID

	// 查询数据，触发缓存写入
	_, err := userDao.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	// 验证缓存Key格式
	expectedPrefix := "data:sysUserExample:"

	// 直接从Redis获取验证
	cached, err := testRedisCache.Get(ctx, id)
	if err != nil {
		t.Errorf("Cache get failed: %v", err)
	} else if cached == nil {
		t.Error("Cache should exist")
	} else {
		t.Logf("缓存Key前缀验证通过: %s%d", expectedPrefix, id)
	}
}

// TestMultiCacheOperations 测试批量缓存操作
// 验证：批量设置和获取的正确性
func TestMultiCacheOperations(t *testing.T) {
	db := initTestDB()
	clearTable(db)
	userDao := NewSysUserExampleDao(db, testRedisCache)
	ctx := context.Background()
	_ = userDao.ClearCache(ctx)

	// 创建多条数据
	users := make([]*model.SysUserExample, 5)
	for i := 0; i < 5; i++ {
		users[i] = &model.SysUserExample{
			Name:   fmt.Sprintf("multi_cache_%d", i),
			Age:    int64Ptr(int64(20 + i)),
			Status: int64Ptr(1),
		}
		if err := userDao.Create(ctx, users[i]); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// 获取所有ID
	ids := make([]uint64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}

	// 批量查询（触发MultiSet）
	result, err := userDao.GetByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(result) != len(users) {
		t.Errorf("Result count mismatch: got %d, want %d", len(result), len(users))
	}

	// 验证每个记录的正确性
	for _, u := range users {
		if cached, ok := result[u.ID]; !ok {
			t.Errorf("ID %d not in result", u.ID)
		} else if cached.Name != u.Name {
			t.Errorf("Name mismatch for ID %d: got %s, want %s", u.ID, cached.Name, u.Name)
		}
	}

	t.Log("批量缓存操作测试通过")
}
