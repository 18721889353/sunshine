# Cache 通用缓存驱动

## 简介

`pkg/cache` 是 Sunshine 微服务框架的底层缓存驱动包，提供了通用的缓存接口和基于 Redis 的实现。

## 架构

```
┌─────────────────────────────────────────┐
│           Cache 接口定义                 │
│    (cache.go - 通用缓存契约)             │
├─────────────────────────────────────────┤
│         Redis 实现                      │
│    (redis.go - 基于 go-redis)           │
│  ┌─────────────┐  ┌──────────────────┐  │
│  │  缓存读写   │  │  分布式锁        │  │
│  │  Set/Get/Del│  │  redsync 实现    │  │
│  └─────────────┘  └──────────────────┘  │
│  ┌─────────────┐  ┌──────────────────┐  │
│  │  批量操作   │  │  占位符防穿透    │  │
│  │  Pipeline   │  │  NotFoundMarker  │  │
│  └─────────────┘  └──────────────────┘  │
└─────────────────────────────────────────┘
```

## 目录结构

```
pkg/cache/
├── cache.go                   # Cache 接口定义
├── redis.go                   # Redis 实现
├── redis_test.go              # 单元测试（miniredis）
├── redis_integration_test.go  # 集成测试（真实 Redis）
└── README.md                  # 本文件
```

## 接口定义

```go
// Cache 通用缓存接口
type Cache interface {
    // 分布式锁
    GetLoopLock(ctx, key, options...) (*Mutex, error)   // 阻塞式获取锁
    GetLock(ctx, key, options...) (*Mutex, error)       // 非阻塞式获取锁
    WatchDogLock(ctx, key, expiry, task, options...) error    // 看门狗锁（非阻塞）
    WatchDogLoopLock(ctx, key, expiry, task, options...) error // 看门狗锁（阻塞）

    // 单条缓存
    Set(ctx, key, val, expireTime) error
    Get(ctx, key, val) error
    Del(ctx, keys...) error

    // 批量操作
    MultiSet(ctx, valMap, expireTime) error
    MultiGet(ctx, keys, valueMap) error

    // 前缀删除
    DelByPrefix(ctx, prefix) error

    // 占位符
    SetCacheWithNotFound(ctx, key) error
}
```

## 快速开始

### 创建 Redis 缓存实例

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/18721889353/sunshine/pkg/cache"
    "github.com/18721889353/sunshine/pkg/encoding"
)

// 创建 Redis 客户端
rdb := redis.NewClient(&redis.Options{
    Addr:     "localhost:6379",
    Password: "",
    DB:       0,
})

// 创建缓存实例
c := cache.NewRedisCache(
    rdb,                    // Redis 客户端
    "",                     // 全局 key 前缀（通常为空）
    encoding.NewJSONEncoding(),  // 序列化编码器
    func() interface{} { return new(MyStruct) },  // 工厂函数
)
```

### 基本使用

```go
ctx := context.Background()

// 写入缓存
err := c.Set(ctx, "user:123", &User{Name: "张三"}, 10*time.Minute)

// 读取缓存
var user User
err := c.Get(ctx, "user:123", &user)

// 删除缓存
err := c.Del(ctx, "user:123")

// 批量写入
err := c.MultiSet(ctx, map[string]interface{}{
    "user:123": &User{Name: "张三"},
    "user:456": &User{Name: "李四"},
}, 10*time.Minute)

// 批量读取
resultMap := make(map[string]interface{})
err := c.MultiGet(ctx, []string{"user:123", "user:456"}, resultMap)

// 按前缀删除
err = c.DelByPrefix(ctx, "user:")

// 设置占位符（防穿透）
err = c.SetCacheWithNotFound(ctx, "user:999")
```

### 分布式锁

```go
// 阻塞式获取锁（循环等待）
lock, err := c.GetLoopLock(ctx, "lock:order:123")
if err == nil {
    defer lock.UnlockContext(ctx)
    // 执行业务逻辑
}

// 非阻塞式获取锁（尝试一次）
lock, err := c.GetLock(ctx, "lock:order:123")
if err != nil {
    // 锁被占用，快速失败
}

// 看门狗锁（自动续期）
err = c.WatchDogLock(ctx, "lock:order:123", 30*time.Second, func(ctx context.Context) error {
    // 执行长时间任务，锁会自动续期
    return doSomething(ctx)
})

// 看门狗锁（阻塞式）
err = c.WatchDogLoopLock(ctx, "lock:order:123", 30*time.Second, func(ctx context.Context) error {
    return doSomething(ctx)
})
```

## Key 前缀

```go
// BuildCacheKey 构建完整的缓存 key
cacheKey, err := cache.BuildCacheKey("prefix:", "user:123")
// 返回: "prefix:user:123"

// BuildLockKey 构建分布式锁 key
lockKey, err := cache.BuildLockKey("prefix:", "lock:user:123")
// 返回: "prefix:lock:user:123"
```

## 占位符机制

当查询的数据不存在时，可以设置占位符防止缓存穿透：

```go
// 设置占位符
err := c.SetCacheWithNotFound(ctx, "user:999")

// 读取时会返回 ErrPlaceholder
var user User
err := c.Get(ctx, "user:999", &user)
if errors.Is(err, cache.ErrPlaceholder) {
    // 数据不存在，返回空或默认值
}
```

## 错误类型

| 错误 | 说明 |
|------|------|
| `CacheNotFound` | key 不存在（redis.Nil） |
| `ErrPlaceholder` | 命中占位符（数据不存在） |

## 配置说明

```go
// NewRedisCacheOption 配置选项（预留）
type NewRedisCacheOption func(*redisCache)

// redisCache 内部配置
type redisCache struct {
    client            *redis.Client      // Redis 客户端
    KeyPrefix         string             // 全局 key 前缀
    encoding          encoding.Encoding  // 序列化编码器
    DefaultExpireTime time.Duration      // 默认过期时间（5秒）
    newObject         func() interface{} // 工厂函数
    redsSync          *redsync.Redsync   // 分布式锁同步器
}
```

## 注意事项

1. **序列化要求**：传入 `Set` 的值必须是可序列化的（结构体指针等）
2. **反序列化要求**：`Get` 的 val 参数必须是指针类型
3. **占位符值**：默认使用 `"*"` 作为占位符，可通过 `NotFoundPlaceholder` 修改
4. **锁释放**：获取锁后务必在 `defer` 中释放，避免死锁
5. **看门狗续期**：续期频率默认为过期时间的 1/3

## 测试说明

### 单元测试（快速，无需真实 Redis）

```bash
# 使用 miniredis 内存模拟，快速验证核心逻辑
go test ./pkg/cache/ -v
```

### 集成测试（需要真实 Redis）

```bash
# 设置环境变量（可选）
export TEST_REDIS_ADDR="127.0.0.1:6379"
export TEST_REDIS_PASSWORD=""

# 运行集成测试
go test ./pkg/cache/ -v -tags=integration
```

### 测试策略

| 测试类型 | 环境依赖 | 执行速度 | 覆盖内容 |
|----------|----------|----------|----------|
| 单元测试 | miniredis（内存） | 毫秒级 | 核心逻辑、边界条件 |
| 集成测试 | 真实 Redis | 秒级 | 真实行为、网络、连接池 |

**建议**：
- 日常开发：只跑单元测试（`go test ./pkg/cache/`）
- CI/CD：跑单元测试 + 集成测试（需配置 Redis 环境）
- 发版前：必须跑集成测试验证真实行为
