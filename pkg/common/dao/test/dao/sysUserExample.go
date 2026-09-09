// Package dao 数据访问层
package dao

import (
	"gorm.io/gorm"

	"github.com/18721889353/sunshine/pkg/common/dao/test/model"

	"github.com/18721889353/sunshine/pkg/common/dao"
)

// NewSysUserExampleDao 创建 SysUserExample 的泛型 DAO 实例
// 使用泛型的 dao.NewDao 函数，自动处理 ID 提取器
//
// 参数：
//   - db: GORM 数据库连接
//   - cache: 缓存适配器，实现了 dao.Cache[model.SysUserExample] 接口
//   - opts: 可变配置选项（使用 dao.WithCacheConfig、dao.WithNoCache 等）
//
// 返回：
//   - *dao.BaseDao[model.SysUserExample] 实例
//
// 示例：
//
//	// 基础用法（使用默认缓存配置）
//	userDao := dao.NewSysUserExampleDao(db, userCache)
//
//	// 自定义缓存配置（10 分钟）
//	userDao := dao.NewSysUserExampleDao(db, userCache,
//	    dao.WithCacheConfig[model.SysUserExample](dao.CacheConfig{DefaultExpireTime: 10 * time.Minute}))
//
//	// 禁用缓存
//	userDao := dao.NewSysUserExampleDao(db, nil, dao.WithNoCache[model.SysUserExample]())
func NewSysUserExampleDao(
	db *gorm.DB,
	cache dao.Cache[model.SysUserExample],
	opts ...dao.Option[model.SysUserExample],
) *dao.BaseDao[model.SysUserExample] {
	// 定义更新字段构建器
	// 根据 SysUserExample 表结构，只更新非零值字段
	updateBuilder := func(table *model.SysUserExample) map[string]interface{} {
		if table == nil {
			return nil
		}
		update := map[string]interface{}{}

		if table.Name != "" {
			update["name"] = table.Name
		}
		if table.Password != "" {
			update["password"] = table.Password
		}
		if table.Email != "" {
			update["email"] = table.Email
		}
		if table.Phone != "" {
			update["phone"] = table.Phone
		}
		if table.Avatar != "" {
			update["avatar"] = table.Avatar
		}
		if table.Age != nil {
			update["age"] = *table.Age
		}
		if table.Gender != nil {
			update["gender"] = *table.Gender
		}
		if table.Status != nil {
			update["status"] = *table.Status
		}
		if table.LoginAt != nil {
			update["login_at"] = *table.LoginAt
		}
		// 生成器会根据表结构自动填充字段映射逻辑
		// 示例：
		// if table.Name != "" { update["name"] = table.Name }
		// if table.Age != 0 { update["age"] = table.Age }
		// if !table.UpdatedAt.IsZero() { update["updated_at"] = table.UpdatedAt }
		return update
	}

	// 使用泛型的 NewDao 函数创建 DAO 实例
	return dao.NewDao[model.SysUserExample](
		db,
		cache,
		(&model.SysUserExample{}).TableName(),
		updateBuilder,
		opts...,
	)
}
