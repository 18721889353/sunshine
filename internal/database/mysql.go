package database

import (
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/sgorm"
	"github.com/18721889353/sunshine/pkg/sgorm/mysql"
	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/config"
)

// InitMysql connect mysql
func InitMysql() *sgorm.DB {
	mysqlCfg := config.Get().Database.Mysql
	opts := []mysql.Option{
		mysql.WithMaxIdleConns(mysqlCfg.MaxIdleConns),
		mysql.WithMaxOpenConns(mysqlCfg.MaxOpenConns),
		mysql.WithConnMaxLifetime(time.Duration(mysqlCfg.ConnMaxLifetime) * time.Minute),
		mysql.WithMaxIdleTime(time.Duration(mysqlCfg.MaxIdleTime) * time.Minute),
		mysql.WithSlowThreshold(time.Duration(mysqlCfg.SlowQueryThresholdMs) * time.Millisecond),
	}
	if mysqlCfg.EnableLog {
		opts = append(opts,
			mysql.WithLogging(),
			mysql.WithLogRequestIDKey(string(logger.ContextKeyForRequestID())),
		)
	}

	if config.Get().App.EnableTrace {
		opts = append(opts, mysql.WithEnableTrace())
	}

	// setting mysql slave and master dsn addresses
	// 先剔除空白 DSN：配置里写成 `- ""` 的占位会被驱动解析成本机默认实例(127.0.0.1:3306 且未选库)，
	// 带着它启用读写分离会让启动直接失败或把读流量指向本机空库，因此视同未配置
	slavesDsn := filterEmptyDsn(mysqlCfg.SlavesDsn)
	mastersDsn := filterEmptyDsn(mysqlCfg.MastersDsn)
	if len(slavesDsn) > 0 && len(mastersDsn) > 0 {
		opts = append(opts, mysql.WithRWSeparation(
			slavesDsn,
			mastersDsn...,
		))
	}

	// add custom gorm plugin
	//opts = append(opts, mysql.WithGormPlugin(yourPlugin))

	dsn := utils.AdaptiveMysqlDsn(mysqlCfg.Dsn)
	db, err := mysql.Init(dsn, opts...)
	if err != nil {
		panic("init mysql error: " + err.Error())
	}
	return db
}

// filterEmptyDsn 剔除空白地址，返回真正可用的 DSN 列表
func filterEmptyDsn(dsns []string) []string {
	usable := make([]string, 0, len(dsns))
	for _, dsn := range dsns {
		if strings.TrimSpace(dsn) != "" {
			usable = append(usable, dsn)
		}
	}

	return usable
}
