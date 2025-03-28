// Package mysql provides a gorm driver for mysql.
package mysql

import (
	"database/sql"
	"fmt"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	mysqlDriver "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
	"gorm.io/plugin/dbresolver"
	"log"
	"os"

	"github.com/18721889353/sunshine/pkg/sgorm/dbclose"
	"github.com/18721889353/sunshine/pkg/sgorm/glog"
)

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
	if o.isLog {
		if o.gLog == nil {
			config.Logger = logger.Default.LogMode(o.logLevel)
		} else {
			config.Logger = glog.NewCustomGormLogger(o.gLog, o.requestIDKey, o.logLevel)
		}
	} else {
		config.Logger = logger.Default.LogMode(logger.Silent)
	}

	// print only slow queries
	if o.slowThreshold > 0 {
		config.Logger = logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags), // use the standard output asWriter
			logger.Config{
				SlowThreshold: o.slowThreshold,
				Colorful:      true,
				LogLevel:      logger.Warn, // set the logging level, only above the specified level will output the slow query log
			},
		)
	}

	return config
}

func getDb(dsn string, o *options) (*gorm.DB, error) {

	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(o.maxIdleConns)       // set the maximum number of connections in the idle connection pool
	sqlDB.SetMaxOpenConns(o.maxOpenConns)       // set the maximum number of open database connections
	sqlDB.SetConnMaxLifetime(o.connMaxLifetime) // set the maximum time a connection can be reused

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

// Close close gorm db
func Close(db *gorm.DB) error {
	return dbclose.Close(db)
}
