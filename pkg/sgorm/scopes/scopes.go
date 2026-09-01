// Package scopes 提供通用的 GORM Scope 函数
package scopes

import (
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

type Scope = func(*gorm.DB) *gorm.DB

// ForceMaster 强制使用主库
func ForceMaster() Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Clauses(dbresolver.Write)
	}
}

// UseReplica 强制使用从库
func UseReplica() Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Clauses(dbresolver.Read)
	}
}

// Unscoped 忽略软删除
func Unscoped() Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Unscoped()
	}
}

// Preload 预加载关联
func Preload(relation string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Preload(relation)
	}
}

// PreloadWithCondition 预加载关联并带条件
func PreloadWithCondition(relation string, conds ...interface{}) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Preload(relation, conds...)
	}
}

// PreloadAll 预加载多个关联
func PreloadAll(relations ...string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		for _, rel := range relations {
			db = db.Preload(rel)
		}
		return db
	}
}

// Select 指定查询字段
func Select(fields ...string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Select(fields)
	}
}

// Omit 排除指定字段
func Omit(fields ...string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Omit(fields...)
	}
}

// Order 排序
func Order(value interface{}) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Order(value)
	}
}

// OrderDesc 倒序排序
func OrderDesc(field string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Order(field + " DESC")
	}
}

// OrderAsc 正序排序
func OrderAsc(field string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Order(field + " ASC")
	}
}

// Limit 限制返回条数
func Limit(limit int) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Limit(limit)
	}
}

// Offset 偏移量
func Offset(offset int) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Offset(offset)
	}
}

// Page 分页（使用参数）
func Page(page, limit int, sort string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		order, l, offset := convertPage(page, limit, sort)
		return db.Order(order).Limit(l).Offset(offset)
	}
}

func convertPage(page, limit int, sort string) (order string, l int, offset int) {
	if page < 0 {
		page = 0
	}
	maxSize := 1000
	if limit > maxSize || limit < 1 {
		limit = maxSize
	}
	if sort == "" {
		sort = "id DESC"
	}
	return sort, limit, page * limit
}

// Where 自定义条件
func Where(query interface{}, args ...interface{}) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(query, args...)
	}
}

// WhereIn 字段在指定值列表中
func WhereIn(field string, values ...interface{}) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(field+" IN (?)", values)
	}
}

// WhereLike 模糊匹配
func WhereLike(field string, pattern string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(field+" LIKE ?", pattern)
	}
}

// WhereBetween 区间查询
func WhereBetween(field string, start, end interface{}) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(field+" BETWEEN ? AND ?", start, end)
	}
}

// WithTx 在事务上下文中执行
func WithTx(tx *gorm.DB) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return tx
	}
}

// Debug 开启调试模式
func Debug() Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Debug()
	}
}

// WithDefaultPagination 默认分页配置（按 ID 倒序，每页20条）
func WithDefaultPagination(page int) Scope {
	return func(db *gorm.DB) *gorm.DB {
		if page < 0 {
			page = 0
		}
		return db.Order("id DESC").Limit(20).Offset(page * 20)
	}
}

// WithFullPagination 完整分页配置
func WithFullPagination(page, limit int, sort string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		order, l, offset := convertPage(page, limit, sort)
		return db.Order(order).Limit(l).Offset(offset)
	}
}

// WithReadReplica 从库读取（应用从库策略 + 其他 Scope）
func WithReadReplica(scopes ...Scope) Scope {
	return func(db *gorm.DB) *gorm.DB {
		db = db.Clauses(dbresolver.Read)
		for _, s := range scopes {
			db = s(db)
		}
		return db
	}
}

// WithWriteMaster 主库写入（应用主库策略 + 其他 Scope）
func WithWriteMaster(scopes ...Scope) Scope {
	return func(db *gorm.DB) *gorm.DB {
		db = db.Clauses(dbresolver.Write)
		for _, s := range scopes {
			db = s(db)
		}
		return db
	}
}
