# 泛型 DAO 缓存框架

## 简介

`pkg/common/dao` 是 Sunshine 微服务框架的核心数据访问层，提供了一套基于 Go 泛型的 ORM 缓存框架。

通过嵌入 `BaseDao[T]` 基类，业务表 DAO 可以自动获得以下能力：
- 单条/批量/分页/条件查询（自动缓存）
- 创建/更新/删除（自动缓存清理 + 延迟双删）
- 分布式锁 + singleflight 防缓存击穿
- 占位符防缓存穿透
- 随机过期时间防缓存雪崩

## 架构图

```
┌─────────────────────────────────────────────────────────────┐
│                      业务层 (UserDao)                        │
├─────────────────────────────────────────────────────────────┤
│                    BaseDao[T] (泛型基类)                      │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────────┐  │
│  │  CRUD 方法   │  │ 缓存管理器   │  │  查询选项处理     │  │
│  └─────────────┘  └──────────────┘  └───────────────────┘  │
├─────────────────────────────────────────────────────────────┤
│              cacheManager[T] (缓存调度大脑)                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │ singleflight │  │  分布式锁    │  │  延迟双删        │  │
│  └──────────────┘  └──────────────┘  └──────────────────┘  │
├─────────────────────────────────────────────────────────────┤
│              CacheAdapter[T] (缓存适配器)                     │
├─────────────────────────────────────────────────────────────┤
│              cache.Cache (底层 Redis 实现)                    │
└─────────────────────────────────────────────────────────────┘
```

## 目录结构

```
pkg/common/dao/
├── base.go              # BaseDao 泛型基类（入口）
├── cache.go             # Cache[T] 泛型缓存接口定义
├── cache_constants.go   # 缓存相关常量定义
├── cache_manager.go     # 缓存管理器（核心调度逻辑）
├── adapter.go           # CacheAdapter 缓存适配器
├── config.go            # CacheConfig 缓存配置
├── options.go           # 查询选项和 DAO 配置选项
├── utils.go             # 工具函数（SQL转换、反射、命名转换等）
├── README.md            # 本文件
└── test/                # 集成测试目录
    ├── dao/             # DAO 集成测试
    └── model/           # 测试用模型
```

## 快速开始

### 1. 定义模型

```go
type User struct {
    ID        uint64         `gorm:"primaryKey"`
    Name      string         `gorm:"column:name"`
    Email     string         `gorm:"column:email"`
    Status    int64          `gorm:"column:status"`
    CreatedAt time.Time
    UpdatedAt time.Time
    DeletedAt gorm.DeletedAt `gorm:"index"`
}
```

### 2. 创建缓存适配器

```go
import (
    "github.com/18721889353/sunshine/pkg/cache"
    "github.com/18721889353/sunshine/pkg/common/dao"
)

// 创建缓存适配器
userCache := dao.NewCacheAdapter[model.User](
    redisCache,                    // Redis 缓存实例
    "data:User",                   // 前缀（会自动加冒号）
    func() interface{} { return new(model.User) },  // 工厂函数
)
```

### 3. 创建 DAO

```go
// 更新字段构建器
updateBuilder := func(u *model.User) map[string]interface{} {
    update := map[string]interface{}{}
    if u.Name != "" {
        update["name"] = u.Name
    }
    if u.Email != "" {
        update["email"] = u.Email
    }
    if u.Status != 0 {
        update["status"] = u.Status
    }
    return update
}

// 创建 DAO（使用默认缓存配置）
userDao := dao.NewDao(db, userCache, "users", updateBuilder)

// 或者自定义缓存配置
userDao := dao.NewDao(db, userCache, "users", updateBuilder,
    dao.WithCacheConfig[model.User](dao.CacheConfig{
        DefaultExpireTime: 10 * time.Minute,
        MaxBatchSize:      500,
    }),
)

// 或者禁用缓存
userDao := dao.NewDao(db, nil, "users", updateBuilder, dao.WithNoCache[model.User]())
```

### 4. 使用 DAO

```go
// 单条查询（自动缓存）
user, err := userDao.GetByID(ctx, 123)

// 强制走主库
user, err := userDao.GetByID(ctx, 123, dao.WithForceMaster())

// 忽略软删除（查出已删数据）
user, err := userDao.GetByID(ctx, 123, dao.WithUnscoped())

// 批量查询
users, err := userDao.GetByIDs(ctx, []uint64{1, 2, 3, 4, 5})

// 分页查询
users, total, err := userDao.GetByColumns(ctx, &query.Params{
    Page:  0,
    Limit: 20,
    Sort:  "-id",
})

// 条件查询
users, err := userDao.GetByCondition(ctx, &query.Conditions{
    Columns: []query.Column{
        {Name: "status", Value: 1},
    },
})

// 计数查询
count, err := userDao.CountByCondition(ctx, &query.Conditions{
    Columns: []query.Column{
        {Name: "status", Value: 1},
    },
})

// 存在性查询
exists, err := userDao.ExistsByCondition(ctx, &query.Conditions{
    Columns: []query.Column{
        {Name: "email", Value: "test@example.com"},
    },
})

// 创建
user := &model.User{Name: "张三", Email: "test@example.com"}
err := userDao.Create(ctx, user)

// 批量创建
users := []*model.User{
    {Name: "张三", Email: "zhangsan@example.com"},
    {Name: "李四", Email: "lisi@example.com"},
}
err := userDao.CreateInBatches(ctx, users, 100)

// 更新
user.Name = "张三（已修改）"
err := userDao.Update(ctx, user)

// 删除
err := userDao.Delete(ctx, user.ID)
```

## 缓存配置

### 默认配置

```go
dao.CacheConfig{
    DefaultExpireTime:          30 * time.Minute,  // 正常缓存 30 分钟
    DefaultNotFoundExpireTime:  1 * time.Minute,   // 占位符 1 分钟
    LockRefreshSleepMs:         50,                 // 锁等待 50ms
    DelayedDeleteInterval:      100 * time.Millisecond,  // 延迟双删 100ms
    MaxCacheableRecords:        1000,               // 分页最多缓存 1000 条
    MaxCacheableIDs:            10000,              // 条件查询最多缓存 10000 个 ID
    MaxBatchSize:               1000,               // 批量操作每批 1000 个
    PlaceholderValue:           "*",                // 占位符标记
}
```

### 预设配置

```go
// 长时间缓存（2 小时）- 适用于低频变更数据
dao.NewDao(db, cache, "dict", updateBuilder, dao.WithLongCache[model.Dict]())

// 短时间缓存（5 分钟）- 适用于高频变更数据
dao.NewDao(db, cache, "stock", updateBuilder, dao.WithShortCache[model.Stock]())

// 禁用缓存
dao.NewDao(db, nil, "log", updateBuilder, dao.WithNoCache[model.Log]())
```

## 缓存策略说明

### 缓存键格式

```
单条记录：data:{表名}:{ID}
条件查询：data:{表名}:condition:{MD5}
分页查询：data:{表名}:columns:{MD5}:total
          data:{表名}:columns:{MD5}:ids
计数查询：data:{表名}:count:{MD5}
存在查询：data:{表名}:exists:{MD5}
占位符：  data:{表名}:{ID}  (值为 "*")
```

### 防缓存穿透

当查询的 ID 在数据库中不存在时，会设置一个短时占位符（默认 1 分钟），防止恶意请求穿透到数据库。

### 防缓存击穿

当热点 Key 过期时，使用 singleflight + 分布式锁保证只有一个请求去查数据库，其他请求等待结果。

### 防缓存雪崩

缓存过期时间在基础值上随机浮动 ±5 分钟，避免大量 Key 同时失效。

### 缓存一致性

采用"延迟双删"策略：更新数据后立即删缓存，100ms 后再删一次，保证最终一致性。

## 工具函数

```go
// SQL 转 COUNT SQL
countSQL, err := dao.ConvertToCountSQL(ctx, "SELECT id FROM users WHERE status = 1")

// 构建分布式锁 Key
lockKey := dao.BuildLockKey("lock:refresh", "123")  // -> "lock:refresh:123"

// 反射获取对象 ID
id := dao.GetObjectID(&model.User{ID: 123})  // -> 123

// 蛇形转驼峰
camel := dao.UnderscoreToCamel("sys_user_example")  // -> "SysUserExample"

// 生成随机过期时间（防雪崩）
expire := dao.GetRandomExpireTime(30 * time.Minute)  // -> 25~35 分钟
```

## 常量说明

| 常量 | 值 | 说明 |
|------|-----|------|
| `CacheKeyPrefixCondition` | `"condition:"` | 条件查询缓存前缀 |
| `CacheKeyPrefixColumns` | `"columns:"` | 分页查询缓存前缀 |
| `CacheKeyPrefixExists` | `"exists:"` | 存在性查询缓存前缀 |
| `CacheKeyPrefixCount` | `"count:"` | 计数查询缓存前缀 |
| `DeleteDaoTypeSingle` | `"single"` | 删除单个 ID 缓存 |
| `DeleteDaoTypeCondition` | `"condition"` | 删除条件查询相关缓存 |
| `DeleteDaoTypeAll` | `"all"` | 删除该表所有缓存 |
| `SortIgnoreCount` | `"ignore count"` | 分页查询跳过 COUNT |

## 最佳实践

1. **模型定义**：ID 字段必须是 `uint64` 类型
2. **更新器**：只返回需要更新的字段，零值字段会被忽略
3. **批量操作**：大批量数据会自动分批处理（默认每批 1000）
4. **软删除**：使用 `WithUnscoped()` 查询已删除的数据
5. **缓存配置**：根据业务特点选择合适的过期时间

## 注意事项

1. 缓存适配器的前缀会自动添加冒号，无需手动添加
2. `GetByID` 默认走主库，确保刚写入的数据能读到
3. 分页查询超过 `MaxCacheableRecords` 条时不会缓存
4. 条件查询超过 `MaxCacheableIDs` 个 ID 时不会缓存
5. 批量查询超过 `MaxBatchSize` 时会自动分批
