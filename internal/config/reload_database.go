package config

import (
	"context"
	"database/sql"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sgorm/glog"
)

// sqlDBGetter 用于获取底层 sql.DB 实例的函数，避免循环导入。
// 必须通过 SetSQLDBGetter 注册后才可使用，未注册时热更新会跳过并输出警告日志。
var sqlDBGetter func() (*sql.DB, error)

// SetSQLDBGetter 设置获取 sql.DB 实例的函数，用于数据库连接池热更新。
// 必须在 database.InitDB() 之后调用。
func SetSQLDBGetter(getter func() (*sql.DB, error)) {
	sqlDBGetter = getter
}

// reloadDatabasePool 数据库连接池参数热更新回调。
// 当 Nacos 配置中的 database.mysql 连接池参数发生变更时，动态调整连接池配置，无需重启服务。
// 支持的参数：maxIdleConns（最大空闲连接数）、maxOpenConns（最大打开连接数）、
// connMaxLifetime（连接最大生命周期）、maxIdleTime（空闲连接最大存活时间）。
// 注意：必须在 SetSQLDBGetter 注册后才生效，否则跳过更新。
func reloadDatabasePool(oldCfg, newCfg *Config) {
	ctx := context.Background()
	oldMysql := oldCfg.Database.Mysql
	newMysql := newCfg.Database.Mysql

	// 1. 检查连接池参数是否发生变更，未变更则跳过
	if oldMysql.MaxIdleConns == newMysql.MaxIdleConns &&
		oldMysql.MaxOpenConns == newMysql.MaxOpenConns &&
		oldMysql.ConnMaxLifetime == newMysql.ConnMaxLifetime &&
		oldMysql.MaxIdleTime == newMysql.MaxIdleTime {
		return
	}

	// 2. sqlDBGetter 未注册时跳过更新
	if sqlDBGetter == nil {
		logger.WarnWithCtx(ctx, "[config reload] sqlDBGetter 未注册，跳过数据库连接池配置更新")
		return
	}

	// 3. 获取 sql.DB 实例，失败时跳过更新
	sqlDB, err := sqlDBGetter()
	if err != nil {
		logger.WarnWithCtx(ctx, "[config reload] 获取 sql.DB 实例失败",
			logger.Err(err),
		)
		return
	}
	if sqlDB == nil {
		logger.WarnWithCtx(ctx, "[config reload] sql.DB 实例为 nil，跳过数据库连接池配置更新")
		return
	}

	// 4. 逐个参数检查并动态更新连接池配置
	if oldMysql.MaxIdleConns != newMysql.MaxIdleConns {
		sqlDB.SetMaxIdleConns(newMysql.MaxIdleConns)
	}
	if oldMysql.MaxOpenConns != newMysql.MaxOpenConns {
		sqlDB.SetMaxOpenConns(newMysql.MaxOpenConns)
	}
	if oldMysql.ConnMaxLifetime != newMysql.ConnMaxLifetime {
		sqlDB.SetConnMaxLifetime(time.Duration(newMysql.ConnMaxLifetime) * time.Minute)
	}
	if oldMysql.MaxIdleTime != newMysql.MaxIdleTime {
		sqlDB.SetConnMaxIdleTime(time.Duration(newMysql.MaxIdleTime) * time.Minute)
	}

	logger.InfoWithCtx(ctx, "[config reload] 数据库连接池配置已更新",
		logger.Int("maxIdleConns", newMysql.MaxIdleConns),
		logger.Int("maxOpenConns", newMysql.MaxOpenConns),
		logger.Int("connMaxLifetime", newMysql.ConnMaxLifetime),
		logger.Int("maxIdleTime", newMysql.MaxIdleTime),
	)
}

// reloadGormLogger GORM 日志配置热更新回调。
// 当 Nacos 配置中的 database.mysql.enableLog 或 slowQueryThresholdMs 发生变更时，
// 动态调整 GORM 日志行为，无需重启服务。
// enableLog 控制是否输出 SQL 日志，slowQueryThresholdMs 控制慢查询阈值（毫秒）。
func reloadGormLogger(oldCfg, newCfg *Config) {
	ctx := context.Background()
	oldMysql := oldCfg.Database.Mysql
	newMysql := newCfg.Database.Mysql

	// 1. 检查 GORM 日志参数是否发生变更，未变更则跳过
	if oldMysql.EnableLog == newMysql.EnableLog &&
		oldMysql.SlowQueryThresholdMs == newMysql.SlowQueryThresholdMs {
		return
	}

	// 2. 更新 SQL 日志开关
	glog.DynamicGormLogger.SetEnableLog(newMysql.EnableLog)

	// 3. 更新慢查询阈值：0 表示禁用慢查询检测
	if newMysql.SlowQueryThresholdMs > 0 {
		glog.DynamicGormLogger.SetSlowThreshold(time.Duration(newMysql.SlowQueryThresholdMs) * time.Millisecond)
	} else {
		glog.DynamicGormLogger.SetSlowThreshold(0)
	}

	logger.InfoWithCtx(ctx, "[config reload] GORM 日志配置已更新",
		logger.Bool("enableLog", newMysql.EnableLog),
		logger.Int("slowQueryThresholdMs", newMysql.SlowQueryThresholdMs),
	)
}
