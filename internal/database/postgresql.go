package database

import (
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sgorm"
	"github.com/18721889353/sunshine/pkg/sgorm/postgresql"
	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/config"
)

// InitPostgresql connect postgresql
func InitPostgresql() *sgorm.DB {
	postgresqlCfg := config.Get().Database.Postgresql
	opts := []postgresql.Option{
		postgresql.WithMaxIdleConns(postgresqlCfg.MaxIdleConns),
		postgresql.WithMaxOpenConns(postgresqlCfg.MaxOpenConns),
		postgresql.WithConnMaxLifetime(time.Duration(postgresqlCfg.ConnMaxLifetime) * time.Minute),
	}
	if postgresqlCfg.EnableLog {
		opts = append(opts,
			postgresql.WithLogging(logger.Get()),
			postgresql.WithLogRequestIDKey("request_id"),
		)
	}

	if config.Get().App.EnableTrace {
		opts = append(opts, postgresql.WithEnableTrace())
	}

	// add custom gorm plugin
	//opts = append(opts, postgresql.WithGormPlugin(yourPlugin))

	dsn := utils.AdaptivePostgresqlDsn(postgresqlCfg.Dsn)
	db, err := postgresql.Init(dsn, opts...)
	if err != nil {
		panic("init postgresql error: " + err.Error())
	}
	return db
}
