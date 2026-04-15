// Package mysql provides a gorm driver for mysql.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
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
		logMode = glog.NewCustomGormLogger(o.requestIDKey, o.logLevel)
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
		err = db.Use(otelgorm.NewPlugin(
			otelgorm.WithoutMetrics(), // 禁用指标收集（可选）
			otelgorm.WithDBName("mysql"),
		))
		if err != nil {
			return nil, fmt.Errorf("using gorm opentelemetry, err: %v", err)
		}
		// 注册自定义 Callback 以增强 Trace 信息
		registerEnhancedTraceCallback(db)
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

// registerEnhancedTraceCallback 注册增强的 Trace Callback，优化 Span 名称和属性
// 注意：
// - Query 操作：otelgorm 使用 Before("gorm:query") 创建 span，我们需要在其之后执行
// - 其他操作：otelgorm 使用 otel:before:xxx 创建 span
// GORM 的 Before 回调是逆序执行（后注册的后执行），所以我们的 Before 会在 otel 之后执行
func registerEnhancedTraceCallback(db *gorm.DB) {
	// 查询操作：在 otel 创建 span 后（Before 逆序执行）、SQL 执行前注入属性
	db.Callback().Query().Before("gorm:query").Register("gorm:trace:enhance:query", func(db *gorm.DB) {
		enhanceSpanWithQueryInfo(db.Statement.Context, db, "query")
	})
	// 创建操作
	db.Callback().Create().After("otel:before:create").Before("otel:after:create").Register("gorm:trace:enhance:create", func(db *gorm.DB) {
		enhanceSpanWithQueryInfo(db.Statement.Context, db, "create")
	})
	// 更新操作
	db.Callback().Update().After("otel:before:update").Before("otel:after:update").Register("gorm:trace:enhance:update", func(db *gorm.DB) {
		enhanceSpanWithQueryInfo(db.Statement.Context, db, "update")
	})
	// 删除操作
	db.Callback().Delete().After("otel:before:delete").Before("otel:after:delete").Register("gorm:trace:enhance:delete", func(db *gorm.DB) {
		enhanceSpanWithQueryInfo(db.Statement.Context, db, "delete")
	})
	// 原始 SQL 操作
	db.Callback().Row().After("otel:before:row").Before("otel:after:row").Register("gorm:trace:enhance:row", func(db *gorm.DB) {
		enhanceSpanWithQueryInfo(db.Statement.Context, db, "row")
	})
	db.Callback().Raw().After("otel:before:raw").Before("otel:after:raw").Register("gorm:trace:enhance:raw", func(db *gorm.DB) {
		enhanceSpanWithQueryInfo(db.Statement.Context, db, "raw")
	})
}

// enhanceSpanWithQueryInfo 增强 Span 信息：重命名 Span 并添加诊断属性
func enhanceSpanWithQueryInfo(ctx context.Context, db *gorm.DB, operation string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	// 1. 提取 request_id（使用统一的 ContextRequestIDKey）
	if reqID := ctx.Value(middleware.ContextRequestIDKey); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			span.SetAttributes(attribute.String("request_id", reqIDStr))
		}
	}

	// 2. 获取表名
	tableName := "unknown"
	if db.Statement.Table != "" {
		tableName = db.Statement.Table
	} else if db.Statement.Schema != nil {
		tableName = db.Statement.Schema.Table
	}

	// 3. 获取 SQL 语句（截断过长部分）
	sql := db.Statement.SQL.String()
	if len(sql) > 200 {
		sql = sql[:200] + "..."
	}

	// 4. 设置诊断属性
	span.SetAttributes(
		attribute.String("db.table", tableName),
		attribute.String("db.operation", operation),
		attribute.String("db.statement", sql),
		attribute.Int64("db.rows_affected", db.Statement.RowsAffected),
	)

	// 5. 如果有错误，记录错误信息
	if db.Error != nil {
		span.RecordError(db.Error,
			trace.WithAttributes(
				attribute.String("error.type", "gorm-db-error"),
				attribute.String("error.context", fmt.Sprintf("%s-operation-failed", operation)),
				attribute.String("db.table", tableName),
			),
		)
	}
}

// Close close gorm db
func Close(db *gorm.DB) error {
	return dbclose.Close(db)
}
