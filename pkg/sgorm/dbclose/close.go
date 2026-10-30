// Package dbclose provides a function to close gorm db.
// 负责关闭 gorm 实例持有的全部 database/sql 连接池：除 db.DB() 能取到的主池，
// 还包含通过 RegisterPools 登记的额外池——读写分离的 source/replica 池由 dbresolver 私有持有，
// 调用方无法从 *gorm.DB 反查，不登记就会在退出时遗留连接与 connectionOpener 等常驻协程。
package dbclose

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"gorm.io/gorm"
)

// poolsPluginName 连接池登记表在 gorm Config.Plugins 中的注册名
const poolsPluginName = "sunshine:conn_pools"

// connPools 登记主池之外、需要随 db 一并关闭的连接池。
// 挂在 gorm Config.Plugins 上做实例级登记：Session() 浅拷贝 Config 时共享同一份登记表，
// 因而根实例登记后派生实例同样可见；生命周期随 db 实例结束，不引入包级全局状态。
type connPools struct {
	mu    sync.Mutex
	pools []*sql.DB
}

// Name 返回登记表在 gorm Plugins 中的键名
func (c *connPools) Name() string { return poolsPluginName }

// Initialize 空实现：登记表只承载连接池引用，不参与 gorm 初始化流程
func (c *connPools) Initialize(*gorm.DB) error { return nil }

// registerMu 保护“查存在-注册”这段非原子操作，避免并发登记时重复写 Plugins map
var registerMu sync.Mutex

// RegisterPools 把额外连接池登记到 db 上，Close 会连同主池一起关闭它们。
// 登记的是入参切片的元素引用，调用方后续不再持有这些池的关闭责任；重复登记同一只池是安全的。
// db 为 nil、Config/Plugins 未初始化或 pools 为空时直接返回 nil，便于调用方无条件调用。
// 本函数可并发调用。
func RegisterPools(db *gorm.DB, pools ...*sql.DB) error {
	if len(pools) == 0 || db == nil || db.Config == nil || db.Plugins == nil {
		return nil
	}

	registerMu.Lock()
	defer registerMu.Unlock()

	reg := lookup(db)
	if reg == nil {
		reg = new(connPools)
		if err := db.Use(reg); err != nil {
			// 重名时 Use 返回 ErrRegistered，回读复用既有登记表，不算失败
			existing := lookup(db)
			if existing == nil {
				return fmt.Errorf("dbclose: register connection pool registry failed: %w", err)
			}
			reg = existing
		}
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.pools = append(reg.pools, pools...)
	return nil
}

// lookup 读取 db 上已登记的连接池表，未登记、Config/Plugins 未初始化或键名被其他类型占用时返回 nil
func lookup(db *gorm.DB) *connPools {
	if db == nil || db.Config == nil || db.Plugins == nil {
		return nil
	}

	plugin, ok := db.Plugins[poolsPluginName]
	if !ok {
		return nil
	}

	reg, ok := plugin.(*connPools)
	if !ok {
		return nil
	}

	return reg
}

// Pools 返回 db 持有的全部连接池：主池在前，随后是按登记顺序的额外池，已按指针去重并剔除 nil。
// 返回的是新建切片，调用方可自由修改而不影响登记表，但不应对其中的池执行关闭，关闭由 Close 负责。
// 仅用于诊断与测试；db 为 nil 时返回空切片。
func Pools(db *gorm.DB) ([]*sql.DB, error) {
	if db == nil {
		return nil, nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	pools := []*sql.DB{sqlDB}
	if reg := lookup(db); reg != nil {
		reg.mu.Lock()
		pools = append(pools, reg.pools...)
		reg.mu.Unlock()
	}

	return dedupPools(pools), nil
}

// Close 关闭主池以及所有已登记的额外池，返回关闭过程中的聚合错误。
// 不等待在用连接归还：sql.DB.Close 自身会阻止新查询并等待已在服务端开始处理的查询结束，
// 额外轮询只会拖慢退出流程。
func Close(db *gorm.DB) error {
	pools, err := Pools(db)
	if err != nil {
		return err
	}

	var closeErr error
	for _, pool := range pools {
		if err := pool.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}

	return closeErr
}

// dedupPools 按指针去重并剔除 nil，保持首次出现的顺序
func dedupPools(pools []*sql.DB) []*sql.DB {
	seen := make(map[*sql.DB]struct{}, len(pools))
	unique := make([]*sql.DB, 0, len(pools))
	for _, pool := range pools {
		if pool == nil {
			continue
		}
		if _, ok := seen[pool]; ok {
			continue
		}
		seen[pool] = struct{}{}
		unique = append(unique, pool)
	}

	return unique
}
