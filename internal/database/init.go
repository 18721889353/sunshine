// Package database provides database client initialization.
package database

import (
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/bwmarrin/snowflake"
	"strings"
	"sync"

	"github.com/18721889353/sunshine/pkg/sgorm"

	"github.com/18721889353/sunshine/internal/config"
)

var (
	gdb     *sgorm.DB
	gdbOnce sync.Once

	snowNode *snowflake.Node
	once4    sync.Once

	ErrRecordNotFound = sgorm.ErrRecordNotFound
)

// todo generate initialisation database code here
// delete the templates code start

// InitDB connect database
func InitDB() {
	dbDriver := config.Get().Database.Driver
	switch strings.ToLower(dbDriver) {
	case sgorm.DBDriverMysql, sgorm.DBDriverTidb:
		gdb = InitMysql()
	case sgorm.DBDriverPostgresql:
		gdb = InitPostgresql()
	case sgorm.DBDriverSqlite:
		gdb = InitSqlite()
	default:
		panic("InitDB error, please modify the correct 'database' configuration at yaml file. " +
			"Refer to https://github.com/18721889353/sunshine/blob/main/configs/serverNameExample.yml#L85")
	}
}

// delete the templates code end

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

//--------------------------------------------------------------------------------------------

// GetSnowNode get db
func GetSnowNode() *snowflake.Node {
	if snowNode == nil {
		once4.Do(func() {
			InitSnowNode()
		})
	}
	return snowNode
}

// InitSnowNode connect redis
func InitSnowNode() {
	node, err := snowflake.NewNode(int64(config.Get().App.MachineID))
	if err != nil {
		logger.Error("snowflake.NewNode err", logger.Err(err))
		panic("snowflake.NewNode error: " + err.Error())
	}
	snowNode = node
}

func GetSnowId() snowflake.ID {
	return snowNode.Generate()
}
