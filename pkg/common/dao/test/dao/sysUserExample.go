// Package dao 数据访问层（DAO）
//
// 负责封装数据库操作，提供统一的数据访问接口。
// 本包使用泛型泛型架构，通过嵌入 dao.BaseDao 获得通用 CRUD 能力，
// 同时支持在业务 DAO 中添加自定义查询方法。
package dao

import (
	"gorm.io/gorm"

	"github.com/18721889353/sunshine/pkg/common/dao"
	"github.com/18721889353/sunshine/pkg/common/dao/test/model"
)

// 编译时检查：确保 SysUserExampleDao 实现了 SysUserExampleDaoInterface 接口
var _ SysUserExampleDaoInterface = (*SysUserExampleDao)(nil)

// SysUserExampleDaoInterface SysUserExample 业务 DAO 接口
//
// 设计思路：
//   - 嵌入 dao.BaseDaoInterface[model.SysUserExample]，自动继承所有泛型方法：
//     GetByID、GetByIDs、Create、Update、Del、GetByColumns、CountByCondition 等
//   - 如需添加自定义方法，在此接口中声明并在 SysUserExampleDao 中实现
//   - 便于依赖注入、单元测试时 Mock 替换
//
// 使用示例：
//
//	type UserService struct {
//	    dao SysUserExampleDaoInterface  // 依赖注入
//	}
//
//	func (s *UserService) GetUser(ctx context.Context, id uint64) (*model.SysUserExample, error) {
//	    return s.dao.GetByID(ctx, id)
//	}
type SysUserExampleDaoInterface interface {
	dao.BaseDaoInterface[model.SysUserExample]

	// 此处可添加自定义方法声明（如 GetByEmail）
	// GetByEmail(ctx context.Context, email string) (*model.SysUserExample, error)
}

// SysUserExampleDao SysUserExample 的扩展 DAO 实现
//
// 设计思路：
//   - 嵌入 *dao.BaseDao[model.SysUserExample]，自动继承所有泛型方法：
//     GetByID、GetByIDs、Create、Update、Del、GetByColumns、CountByCondition 等
//   - 当内置方法无法满足业务需求时，直接在此结构体上添加自定义方法
//   - 类型安全，编译时检查，IDE 可自动补全
//
// 使用示例：
//
//	dao := NewSysUserExampleDao(db, cache)
//	user, _ := dao.GetByID(ctx, 1)           // 使用泛型方法
//	user, _ = dao.GetByEmail(ctx, "a@b.com") // 使用自定义方法
type SysUserExampleDao struct {
	*dao.BaseDao[model.SysUserExample]
}

// NewSysUserExampleDao 创建 SysUserExample 的 DAO 实例
//
// 参数说明：
//   - db: GORM 数据库连接实例
//   - cache: 缓存适配器，实现 dao.Cache[model.SysUserExample] 接口；传 nil 则禁用缓存
//   - opts: 可选配置项，支持 dao.WithCacheConfig、dao.WithNoCache 等
//
// 返回值：
//   - *SysUserExampleDao: 扩展 DAO 实例，可直接调用 GetByID 等泛型方法和 GetByEmail 等自定义方法
//
// 示例：
//
//	// 基础用法（使用默认缓存配置）
//	userDao := dao.NewSysUserExampleDao(db, userCache)
//	user, _ := userDao.GetByID(ctx, 1)            // 泛型方法
//	user, _ = userDao.GetByEmail(ctx, "a@b.com")   // 自定义方法
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
) *SysUserExampleDao {
	// 定义更新字段构建器：控制 UPDATE 语句只更新非零值字段
	updateBuilder := func(table *model.SysUserExample) map[string]interface{} {
		if table == nil {
			return nil
		}
		update := map[string]interface{}{}
		// 示例：
		// if table.Name != "" { update["name"] = table.Name }
		// if table.Age != 0 { update["age"] = table.Age }
		//生成器会根据表结构自动填充字段映射逻辑

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
		return update
	}

	return &SysUserExampleDao{
		BaseDao: dao.NewDao[model.SysUserExample](
			db,
			cache,
			(&model.SysUserExample{}).TableName(),
			"",
			updateBuilder,
			opts...,
		),
	}
}

// ============================================================================
// 自定义方法区域
// 说明：当 BaseDao 内置方法（GetByID、GetByColumns 等）无法满足需求时，
//       在此区域添加业务专属的查询方法。
// ============================================================================

// GetByEmail 根据邮箱地址查询用户（自定义方法示例）
//
// 使用方式：
//	userDao := NewSysUserExampleDao(db, cache)
//	user, err := userDao.GetByEmail(ctx, "test@example.com")
//
// 参数说明：
//   - ctx: 请求上下文，用于传递超时和取消信号
//   - email: 邮箱地址
//
// 返回值：
//   - *model.SysUserExample: 查询到的用户信息
//   - error: 查询失败时返回错误信息
//
// 注意：此方法不经过缓存，每次调用都会查询数据库
//       如需缓存，可使用 GetByCondition + GetByIDs 组合实现
//func (d *SysUserExampleDao) GetByEmail(ctx context.Context, email string) (*model.SysUserExample, error) {
//	var user model.SysUserExample
//	_, err := d.GetByCustomQuery(ctx, func(db *gorm.DB) *gorm.DB {
//		return db.Where("email = ?", email).First(&user)
//	}, &user, -1, -1) // page=-1, limit=-1 表示不分页
//	if err != nil {
//		return nil, err
//	}
//	return &user, nil
//}
