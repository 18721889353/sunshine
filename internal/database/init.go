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

	ErrRecordNotFound = sgorm.ErrRecordNotFound
)

// InitDB connect database
func InitDB() {
	dbDriver := config.Get().Database.Driver
	switch strings.ToLower(dbDriver) {
	case sgorm.DBDriverMysql:
		gdb = InitMysql()
	default:
		panic("InitDB error, please modify the correct 'database' configuration at yaml file. " +
			"Refer to https://github.com/18721889353/sunshine/blob/main/configs/serverNameExample.yml#L85")
	}
}

// GetDB get db
func GetDB() *sgorm.DB {
	if gdb == nil {
		gdbOnce.Do(func() {
			InitDB()
		})
	}

	return gdb
}

// CloseDB close db
func CloseDB() error {
	return sgorm.CloseDB(gdb)
}
