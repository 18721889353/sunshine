// Package cache 缓存层
//
// 提供业务数据的缓存访问能力，支持 Redis 和本地缓存两种后端。
// 本包使用泛型适配器模式，通过嵌入 dao.CacheAdapter 获得通用缓存操作，
// 同时支持在业务缓存中添加自定义方法。
package cache

import (
	"strings"

	"github.com/18721889353/sunshine/internal/consts"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/pkg/cache"
	"github.com/18721889353/sunshine/pkg/common/dao"
	"github.com/18721889353/sunshine/pkg/common/dao/test/model"
	"github.com/18721889353/sunshine/pkg/encoding"
)

const (
	// SysUserExampleCachePrefixKey SysUserExample 缓存数据的 Redis key 前缀
	// 实际存储格式：data:sysUserExample::{id}
	SysUserExampleCachePrefixKey = "data:sysUserExample:"
)

// 编译时检查：确保 sysUserExampleCache 实现了 SysUserExampleCache 接口
var _ SysUserExampleCache = (*sysUserExampleCache)(nil)

// SysUserExampleCache SysUserExample 业务缓存接口
//
// 设计思路：
//   - 直接嵌入 dao.Cache[model.SysUserExample]，自动继承所有缓存方法
//   - 如需添加自定义缓存方法，在此接口中声明并在 sysUserExampleCache 中实现
//
// 继承的方法包括：
//   - Set/Get/Del: 单条数据的增删改查
//   - MultiSet/MultiGet: 批量数据操作
//   - DelByPrefix: 按前缀批量删除
//   - Lock/Unlock: 分布式锁
type SysUserExampleCache interface {
	dao.Cache[model.SysUserExample]
}

// sysUserExampleCache SysUserExample 缓存实现
//
// 设计思路：
//   - 嵌入 *dao.CacheAdapter[model.SysUserExample]，自动获得所有缓存能力
//   - 无需手写任何透传方法，直接使用泛型适配器的方法
//   - 当需要自定义缓存逻辑时，在此结构体上添加新方法
type sysUserExampleCache struct {
	*dao.CacheAdapter[model.SysUserExample]
}

// NewSysUserExampleCache 创建 SysUserExample 缓存实例
//
// 参数说明：
//   - cacheType: 缓存类型配置，包含 Redis 连接信息和缓存类型标识
//
// 返回值：
//   - SysUserExampleCache: 缓存接口实例，传入 nil 或不支持的类型时返回 nil
//
// 创建流程：
//  1. 创建通用 Redis 驱动（底层缓存操作）
//  2. 创建泛型适配器（将通用驱动适配为 dao.Cache[T] 接口）
//  3. 包装为业务缓存并返回
func NewSysUserExampleCache(cacheType *database.CacheType) SysUserExampleCache {
	cType := strings.ToLower(cacheType.CType)
	if cType != consts.CacheTypeRedis {
		return nil
	}

	// 1. 创建通用 Redis 驱动
	redisDriver := cache.NewRedisCache(
		cacheType.Rdb,
		"", // 全局前缀为空，由适配器管理
		encoding.JSONEncoding{},
		func() interface{} { return &model.SysUserExample{} },
	)

	// 2. 创建泛型适配器，将通用驱动适配为 dao.Cache[T]
	adapter := dao.NewCacheAdapter[model.SysUserExample](
		redisDriver,
		SysUserExampleCachePrefixKey,
		func() interface{} { return &model.SysUserExample{} },
	)

	// 3. 返回业务缓存实例
	return &sysUserExampleCache{
		CacheAdapter: adapter,
	}
}

// ============================================================================
// 自定义缓存方法区域
// 说明：当 CacheAdapter 内置方法无法满足需求时，在此区域添加自定义缓存逻辑。
//       注意：自定义方法需要自行实现缓存读写逻辑。
// ============================================================================

// GetByName 根据用户名获取用户（自定义方法示例）
//
// 注意：此方法不经过泛型适配器，需要自行实现缓存逻辑
//
// 参数说明：
//   - ctx: 请求上下文
//   - name: 用户名
//
// 返回值：
//   - *model.SysUserExample: 查询到的用户信息
//   - error: 查询失败时返回错误信息
//func (c *sysUserExampleCache) GetByName(ctx context.Context, name string) (*model.SysUserExample, error) {
//	// 自定义缓存逻辑
//	cacheKey := "user:name:" + name
//	var user model.SysUserExample
//	err, _ := c.GetIDsByKey(ctx, cacheKey)
//	if err == nil {
//		return &user, nil
//	}
//	// 查数据库...
//	return nil, nil
//}
