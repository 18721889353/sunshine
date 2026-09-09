// Package cache 缓存层
// 提供 Redis 和本地缓存的统一接口，支持分布式锁、防击穿、防穿透等功能
package cache

import (
	"strings"

	"github.com/18721889353/sunshine/internal/consts"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/model"
	"github.com/18721889353/sunshine/pkg/cache"
	"github.com/18721889353/sunshine/pkg/common/dao"
	"github.com/18721889353/sunshine/pkg/encoding"
)

const (
	// UserExampleCachePrefixKey 业务缓存数据的 Redis key 前缀
	UserExampleCachePrefixKey = "data:userExample:"
)

// 编译时检查：确保 userExampleCache 实现了 UserExampleCache 接口
var _ UserExampleCache = (*userExampleCache)(nil)

// UserExampleCache 业务缓存接口
// 直接继承泛型 dao.Cache 接口，自动拥有所有方法
// 如需添加自定义方法，在此接口中声明，并在 userExampleCache 中实现
type UserExampleCache interface {
	dao.Cache[model.UserExample]
	// 示例：自定义方法（根据需要添加）
	// GetByName(ctx context.Context, name string) (*model.UserExample, error)
}

// userExampleCache 通过嵌入泛型适配器获得所有缓存能力
// 无需手写任何透传方法
type userExampleCache struct {
	*dao.CacheAdapter[model.UserExample] // 嵌入泛型适配器
}

// NewUserExampleCache 创建 UserExample 缓存实例
func NewUserExampleCache(cacheType *database.CacheType) UserExampleCache {
	cType := strings.ToLower(cacheType.CType)
	if cType != consts.CacheTypeRedis {
		return nil
	}

	// 1. 创建通用 Redis 驱动（不依赖 dao 包）
	redisDriver := cache.NewRedisCache(
		cacheType.Rdb,
		"", // 全局前缀为空，由适配器管理
		encoding.JSONEncoding{},
		func() interface{} { return &model.UserExample{} },
	)

	// 2. 创建泛型适配器，将通用驱动适配为 dao.Cache[T]
	adapter := dao.NewCacheAdapter[model.UserExample](
		redisDriver,
		UserExampleCachePrefixKey, // 数据前缀（会自动处理冒号）
		func() interface{} { return &model.UserExample{} },
	)

	// 3. 返回嵌入适配器的业务缓存
	return &userExampleCache{
		CacheAdapter: adapter,
	}
}

// ============================================================================
// 如果需要自定义方法，在这里实现（示例）
// ============================================================================

// GetByName 根据用户名获取用户（自定义方法示例）
// 注意：这种方法不经过泛型适配器，需要自己实现缓存逻辑
// func (c *userExampleCache) GetByName(ctx context.Context, name string) (*model.UserExample, error) {
//     // 自定义缓存逻辑
//     cacheKey := "user:name:" + name
//     var user model.UserExample
//     err := c.Get(ctx, cacheKey, &user)
//     if err == nil {
//         return &user, nil
//     }
//     // 查数据库...
//     return nil, nil
// }
