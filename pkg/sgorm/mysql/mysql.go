// Package mysql provides a gorm driver for mysql.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	mysqlDriver "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"gorm.io/plugin/dbresolver"

	"github.com/18721889353/sunshine/pkg/sgorm/dbclose"
	"github.com/18721889353/sunshine/pkg/sgorm/glog"
)

// combinedLogger combines normal logger and slow query logger
type combinedLogger struct {
	normalLogger logger.Interface
	slowLogger   logger.Interface
}

// LogMode log mode
func (c *combinedLogger) LogMode(level logger.LogLevel) logger.Interface {
	return &combinedLogger{
		normalLogger: c.normalLogger.LogMode(level),
		slowLogger:   c.slowLogger.LogMode(level),
	}
}

// Info logs info
func (c *combinedLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	c.normalLogger.Info(ctx, msg, data...)
}

// Warn logs warn
func (c *combinedLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	c.normalLogger.Warn(ctx, msg, data...)
}

// Error logs error
func (c *combinedLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	c.normalLogger.Error(ctx, msg, data...)
}

// Trace logs trace
func (c *combinedLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	// Log with normal logger
	c.normalLogger.Trace(ctx, begin, fc, err)

	// Also log with slow logger to catch slow queries
	c.slowLogger.Trace(ctx, begin, fc, err)
}

// Init mysql
func Init(dsn string, opts ...Option) (*gorm.DB, error) {
	o := defaultOptions()
	o.apply(opts...)
	db, err := getDb(dsn, o)
	if err != nil {
		return nil, fmt.Errorf("getDb, err: %v", err)
	}
	// register read-write separation plugin
	if len(o.slavesDsn) > 0 {
		err = db.Use(rwSeparationPlugin(o))
		if err != nil {
			return nil, err
		}
	}
	return db, nil
}

// InitTidb init tidb
func InitTidb(dsn string, opts ...Option) (*gorm.DB, error) {
	return Init(dsn, opts...)
}

// gorm setting
func gormConfig(o *options) *gorm.Config {
	config := &gorm.Config{
		// disable foreign key constraints, not recommended for production environments
		DisableForeignKeyConstraintWhenMigrating: o.disableForeignKey,
		// removing the plural of an epithet
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	}

	// print SQL
	var logMode logger.Interface
	if o.isLog {
		if o.gLog == nil {
			logMode = logger.Default.LogMode(o.logLevel)
		} else {
			logMode = glog.NewCustomGormLogger(o.gLog, o.requestIDKey, o.logLevel)
		}
	} else {
		logMode = logger.Default.LogMode(logger.Silent)
	}

	// add slow query logging if threshold is set
	if o.slowThreshold > 0 {
		slowLogger := logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags), // use the standard output asWriter
			logger.Config{
				SlowThreshold: o.slowThreshold,
				Colorful:      true,
				LogLevel:      logger.Warn, // set the logging level, only above the specified level will output the slow query log
			},
		)
		// Combine both loggers if both are needed
		if o.isLog {
			// Use the existing logger for normal queries and the slow logger for slow queries
			config.Logger = &combinedLogger{normalLogger: logMode, slowLogger: slowLogger}
		} else {
			// Only log slow queries
			config.Logger = slowLogger
		}
	} else {
		config.Logger = logMode
	}

	return config
}

func getDb(dsn string, o *options) (*gorm.DB, error) {

	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(o.maxIdleConns)       // 设置空闲连接池中最大连接数
	sqlDB.SetMaxOpenConns(o.maxOpenConns)       // 设置数据库最大打开连接数
	sqlDB.SetConnMaxLifetime(o.connMaxLifetime) // 设置连接可重用的最大时间
	sqlDB.SetConnMaxIdleTime(o.maxIdleTime)     // 设置空闲连接的最大空闲时间

	db, err := gorm.Open(mysqlDriver.New(mysqlDriver.Config{Conn: sqlDB}), gormConfig(o))
	if err != nil {
		return nil, err
	}
	db.Set("gorm:table_options", "CHARSET=utf8mb4") // automatic appending of table suffixes when creating tables
	// register trace plugin
	if o.enableTrace {
		err = db.Use(otelgorm.NewPlugin())
		if err != nil {
			return nil, fmt.Errorf("using gorm opentelemetry, err: %v", err)
		}
		// 注册自定义 Callback 以传递 request_id 到 Span 属性
		registerRequestIDCallback(db)
	}
	// register plugins
	for _, plugin := range o.plugins {
		err = db.Use(plugin)
		if err != nil {
			return nil, err
		}
	}
	return db, nil
}

func rwSeparationPlugin(o *options) gorm.Plugin {
	slaves := []gorm.Dialector{}
	for _, dsn := range o.slavesDsn {
		db, err := getDb(dsn, o)
		if err != nil {
			log.Fatalf("Failed to initialize slave database with DSN %s: %v", dsn, err)
		}
		conn, err := db.DB()
		if err != nil {
			log.Fatalf("Failed to get underlying sql.DB for slave with DSN %s: %v", dsn, err)
		}
		slaves = append(slaves, mysqlDriver.New(mysqlDriver.Config{
			Conn: conn,
		}))
	}

	masters := []gorm.Dialector{}
	for _, dsn := range o.mastersDsn {
		db, err := getDb(dsn, o)
		if err != nil {
			log.Fatalf("Failed to initialize master database with DSN %s: %v", dsn, err)
		}
		conn, err := db.DB()
		if err != nil {
			log.Fatalf("Failed to get underlying sql.DB for master with DSN %s: %v", dsn, err)
		}
		masters = append(masters, mysqlDriver.New(mysqlDriver.Config{
			Conn: conn,
		}))
	}

	return dbresolver.Register(dbresolver.Config{
		Sources:  masters,
		Replicas: slaves,
		Policy:   dbresolver.RandomPolicy{},
	})
}

// registerRequestIDCallback 注册自定义 Callback，在 GORM Span 创建后提取 request_id 并设置到 Span 属性
func registerRequestIDCallback(db *gorm.DB) {
	// 查询操作：在 OTel Span 创建后、结束前设置
	db.Callback().Query().After("otel:before:query").Before("otel:after:query").Register("otel:request_id:query", func(db *gorm.DB) {
		setRequestIDToSpan(db.Statement.Context, db)
	})
	// 创建操作
	db.Callback().Create().After("otel:before:create").Before("otel:after:create").Register("otel:request_id:create", func(db *gorm.DB) {
		setRequestIDToSpan(db.Statement.Context, db)
	})
	// 更新操作
	db.Callback().Update().After("otel:before:update").Before("otel:after:update").Register("otel:request_id:update", func(db *gorm.DB) {
		setRequestIDToSpan(db.Statement.Context, db)
	})
	// 删除操作
	db.Callback().Delete().After("otel:before:delete").Before("otel:after:delete").Register("otel:request_id:delete", func(db *gorm.DB) {
		setRequestIDToSpan(db.Statement.Context, db)
	})
	// 原始 SQL 操作
	db.Callback().Row().After("otel:before:row").Before("otel:after:row").Register("otel:request_id:row", func(db *gorm.DB) {
		setRequestIDToSpan(db.Statement.Context, db)
	})
	db.Callback().Raw().After("otel:before:raw").Before("otel:after:raw").Register("otel:request_id:raw", func(db *gorm.DB) {
		setRequestIDToSpan(db.Statement.Context, db)
	})
}

// setRequestIDToSpan 从 Context 提取 request_id 并设置到当前 Span 属性
func setRequestIDToSpan(ctx context.Context, db *gorm.DB) {
	// 从 Context 中提取 request_id
	if reqID := ctx.Value("request_id"); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			// 获取当前 Span 并设置属性
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.SetAttributes(attribute.String("request_id", reqIDStr))
			}
		}
	}
	_ = db // 避免未使用警告（db.Statement.Context 已使用）
}

// Close close gorm db
func Close(db *gorm.DB) error {
	return dbclose.Close(db)
}
