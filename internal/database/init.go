// Package database provides database client initialization.
package database

import (
	"strings"
	"sync"

	"github.com/18721889353/sunshine/internal/config"

	"github.com/18721889353/sunshine/pkg/sgorm"
)

var (
	gdb     *sgorm.DB
	gdbOnce sync.Once

	// ErrRecordNotFound 记录未找到错误
	ErrRecordNotFound = sgorm.ErrRecordNotFound
)

// InitDB connect database
// 幂等：显式调用 InitDB() 与惰性调用 GetDB() 共用同一个 once，
// 重复初始化会让上一套连接池失去引用，既无人使用也无人关闭
func InitDB() {
	gdbOnce.Do(func() {
		dbDriver := config.Get().Database.Driver
		switch strings.ToLower(dbDriver) {
		case sgorm.DBDriverMysql:
			gdb = InitMysql()
		default:
			panic("InitDB error, please modify the correct 'database' configuration at yaml file. " +
				"Refer to https://github.com/18721889353/sunshine/blob/main/configs/serverNameExample.yml#L85")
		}
	})
}

// GetDB get db
func GetDB() *sgorm.DB {
	InitDB()
	return gdb
}

// CloseDB close db
func CloseDB() error {
	return sgorm.CloseDB(gdb)
}
