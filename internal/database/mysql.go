package database

import (
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
	if len(mysqlCfg.SlavesDsn) > 0 && len(mysqlCfg.MastersDsn) > 0 {
		opts = append(opts, mysql.WithRWSeparation(
			mysqlCfg.SlavesDsn,
			mysqlCfg.MastersDsn...,
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
