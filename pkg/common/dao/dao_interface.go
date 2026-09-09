// Package dao 提供泛型数据访问层基础组件
package dao

import (
	"context"

	"gorm.io/gorm"

	"github.com/18721889353/sunshine/pkg/sgorm/query"
)

// ============================================================================
// BaseDaoInterface 泛型 DAO 接口
// 定义了业务 DAO 需要实现的所有方法，便于依赖注入和单元测试 Mock。
// 业务层通过嵌入此接口实现"接口嵌入接口"的设计模式。
// ============================================================================

// BaseDaoInterface 泛型数据访问接口
// 封装了数据库 CRUD 操作和缓存管理的标准方法签名。
// 业务层 DAO 通过嵌入此接口，自动获得所有方法的类型约束。
//
// 使用示例：
//
//	// 在业务层定义接口
//	type UserExampleDaoInterface interface {
//	    dao.BaseDaoInterface[model.UserExample]
//	    // 可添加自定义方法声明
//	}
//
//	// 在 service 中依赖注入
//	type UserService struct {
//	    dao UserExampleDaoInterface
//	}
type BaseDaoInterface[T any] interface {
	// ============================================================
	// 查询方法 - 单条查询
	// ============================================================

	// GetByID 根据 ID 获取单条记录（支持缓存）
	// 参数：ctx 上下文, id 记录ID, opts 查询选项
	// 返回：*T 记录指针, error 错误信息
	GetByID(ctx context.Context, id uint64, opts ...QueryOption) (*T, error)

	// GetOneByColumns 根据列条件获取单条记录（支持缓存）
	// 参数：ctx 上下文, params 分页查询参数, opts 查询选项
	// 返回：*T 记录指针, error 错误信息
	GetOneByColumns(ctx context.Context, params *query.Params, opts ...QueryOption) (*T, error)

	// ============================================================
	// 查询方法 - 分页查询
	// ============================================================

	// GetByColumns 根据列条件进行分页查询（支持缓存）
	// 参数：ctx 上下文, params 分页查询参数, opts 查询选项
	// 返回：[]*T 记录列表, int64 总记录数, error 错误信息
	GetByColumns(ctx context.Context, params *query.Params, opts ...QueryOption) ([]*T, int64, error)

	// ============================================================
	// 查询方法 - 条件查询
	// ============================================================

	// GetByCondition 根据条件获取 ID 列表（支持缓存）
	// 参数：ctx 上下文, c 条件, opts 查询选项
	// 返回：[]uint64 ID列表, error 错误信息
	GetByCondition(ctx context.Context, c *query.Conditions, opts ...QueryOption) ([]uint64, error)

	// GetByIDs 根据 ID 列表批量获取记录（支持缓存）
	// 参数：ctx 上下文, ids ID列表, opts 查询选项
	// 返回：map[uint64]*T ID到记录的映射, error 错误信息
	GetByIDs(ctx context.Context, ids []uint64, opts ...QueryOption) (map[uint64]*T, error)

	// CountByCondition 根据条件统计记录数（支持缓存）
	// 参数：ctx 上下文, c 条件, opts 查询选项
	// 返回：int64 记录数, error 错误信息
	CountByCondition(ctx context.Context, c *query.Conditions, opts ...QueryOption) (int64, error)

	// ExistsByCondition 检查是否存在满足条件的记录（支持缓存）
	// 参数：ctx 上下文, c 条件, opts 查询选项
	// 返回：bool 是否存在, error 错误信息
	ExistsByCondition(ctx context.Context, c *query.Conditions, opts ...QueryOption) (bool, error)

	// GetByCustomQuery 执行自定义查询，支持分页和原始 SQL（无缓存）
	// 参数：ctx 上下文, queryFunc 查询构建函数, result 结果接收器, page 页码, limit 每页条数, opts 查询选项
	// 返回：int64 总记录数, error 错误信息
	GetByCustomQuery(ctx context.Context, queryFunc func(*gorm.DB) *gorm.DB, result interface{}, page, limit int, opts ...QueryOption) (int64, error)

	// ============================================================
	// 创建方法
	// ============================================================

	// Create 创建单条记录
	// 参数：ctx 上下文, entity 记录指针
	// 返回：error 错误信息
	Create(ctx context.Context, entity *T) error

	// CreateInBatches 批量创建记录
	// 参数：ctx 上下文, entities 记录切片, batchSize 批次大小
	// 返回：error 错误信息
	CreateInBatches(ctx context.Context, entities []*T, batchSize int) error

	// CreateByTx 在事务中创建单条记录
	// 参数：ctx 上下文, tx 事务对象, entity 记录指针
	// 返回：uint64 新记录ID, error 错误信息
	CreateByTx(ctx context.Context, tx *gorm.DB, entity *T) (uint64, error)

	// CreateByInBatchesTx 在事务中批量创建记录
	// 参数：ctx 上下文, tx 事务对象, entities 记录切片, batchSize 批次大小
	// 返回：error 错误信息
	CreateByInBatchesTx(ctx context.Context, tx *gorm.DB, entities []*T, batchSize int) error

	// ============================================================
	// 更新方法
	// ============================================================

	// UpdateByID 根据 ID 更新记录
	// 参数：ctx 上下文, entity 记录指针（需包含ID）
	// 返回：error 错误信息
	UpdateByID(ctx context.Context, entity *T) error

	// UpdateByCondition 根据条件批量更新
	// 参数：ctx 上下文, c 条件, entity 更新内容
	// 返回：error 错误信息
	UpdateByCondition(ctx context.Context, c *query.Conditions, entity *T) error

	// UpdateByTx 在事务中根据 ID 更新记录
	// 参数：ctx 上下文, tx 事务对象, entity 记录指针
	// 返回：error 错误信息
	UpdateByTx(ctx context.Context, tx *gorm.DB, entity *T) error

	// UpdateByConditionTx 在事务中根据条件批量更新
	// 参数：ctx 上下文, tx 事务对象, c 条件, entity 更新内容
	// 返回：error 错误信息
	UpdateByConditionTx(ctx context.Context, tx *gorm.DB, c *query.Conditions, entity *T) error

	// ============================================================
	// 删除方法
	// ============================================================

	// DeleteByID 根据 ID 删除记录（支持软删除）
	// 参数：ctx 上下文, id 记录ID
	// 返回：error 错误信息
	DeleteByID(ctx context.Context, id uint64) error

	// DeleteByIDs 根据 ID 列表批量删除（支持软删除）
	// 参数：ctx 上下文, ids ID列表
	// 返回：error 错误信息
	DeleteByIDs(ctx context.Context, ids []uint64) error
}
