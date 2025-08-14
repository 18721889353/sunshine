// Package database provides database client initialization.
package database

import (
	"strings"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/bwmarrin/snowflake"

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
func GetTimeFromSnowId(id snowflake.ID) time.Time {
	// Snowflake ID 的时间部分在 41 位时间戳字段中
	// 需要将 ID 右移 22 位来获取时间戳（机器ID(10位) + 序列号(12位) = 22位）
	timestamp := (int64(id) >> 22) + 1288834974657 // 添加 Twitter Snowflake 的起始时间戳
	return time.Unix(0, timestamp*int64(time.Millisecond))
}

// GetSequenceFromSnowId 从雪花ID中提取序列号
func GetSequenceFromSnowId(id snowflake.ID) int64 {
	// 序列号是雪花ID的最低12位
	sequence := int64(id) & 0xFFF
	return sequence
}

// ParseSnowId 解析雪花ID的各个组成部分
func ParseSnowId(id snowflake.ID) map[string]int64 {
	timestamp := (int64(id) >> 22) + 1288834974657 // Twitter Snowflake起始时间戳
	machineID := (int64(id) >> 12) & 0x3FF
	sequence := int64(id) & 0xFFF

	return map[string]int64{
		"timestamp": timestamp,
		"machineID": machineID,
		"sequence":  sequence,
	}
}
