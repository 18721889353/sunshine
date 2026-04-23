// Package database provides database client initialization.
package database

import (
	"context"
	"errors"
	"fmt"
	"github.com/18721889353/sunshine/internal/config"
	"hash/fnv"
	"net"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/bwmarrin/snowflake"
)

var (
	snowNode *snowflake.Node
	snowOnce sync.Once
)

// GetSnowNode get db
func GetSnowNode() *snowflake.Node {
	if snowNode == nil {
		snowOnce.Do(func() {
			InitSnowNode()
		})
	}
	return snowNode
}

// InitSnowNode connect redis
func InitSnowNode() {
	initCtx := context.Background()
	machineID, err := getMachineID()
	if err != nil {
		logger.ErrorWithCtx(initCtx, "getMachineID err", logger.Err(err))
		panic("getMachineID error: " + err.Error())
	}
	node, err := snowflake.NewNode(machineID)
	if err != nil {
		logger.ErrorWithCtx(initCtx, "snowflake.NewNode err", logger.Err(err))
		panic("snowflake.NewNode error: " + err.Error())
	}
	snowNode = node
}

// 从配置或环境变量中获取MachineID
func getMachineID() (int64, error) {
	// 1. 尝试从配置文件读取
	var machineID int
	if config.Get() != nil {
		machineID = config.Get().App.MachineID
	}
	if machineID <= 0 {
		// 如果未配置，从IP地址计算
		var err error
		machineID, err = calculateMachineIDFromIP()
		if err != nil {
			return 0, fmt.Errorf("failed to calculate machine ID from IP: %w", err)
		}
	}
	// 确保MachineID在合法范围内(0-1023)
	return int64(machineID % 1024), nil
}
func calculateMachineIDFromIP() (int, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return 0, fmt.Errorf("failed to get interface addresses: %w", err)
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				// 简单的IP地址哈希计算
				ipStr := ipNet.IP.String()
				hash := fnv.New32a()
				_, err := hash.Write([]byte(ipStr))
				if err != nil {
					return 0, err
				}
				return int(hash.Sum32()) % 1024, nil
			}
		}
	}
	return 0, errors.New("failed to get interface addresses")
}

// GetSnowID 获取雪花ID
func GetSnowID() snowflake.ID {
	return snowNode.Generate()
}

// GetTimeFromSnowID 从雪花ID中获取时间
func GetTimeFromSnowID(id snowflake.ID) time.Time {
	// Snowflake ID 的时间部分在 41 位时间戳字段中
	// 需要将 ID 右移 22 位来获取时间戳（机器ID(10位) + 序列号(12位) = 22位）
	timestamp := (int64(id) >> 22) + 1288834974657 // 添加 Twitter Snowflake 的起始时间戳
	return time.Unix(0, timestamp*int64(time.Millisecond))
}

// GetSequenceFromSnowID 从雪花ID中提取序列号
func GetSequenceFromSnowID(id snowflake.ID) int64 {
	// 序列号是雪花ID的最低12位
	sequence := int64(id) & 0xFFF
	return sequence
}

// ParseSnowID 解析雪花ID的各个组成部分
func ParseSnowID(id snowflake.ID) map[string]int64 {
	timestamp := (int64(id) >> 22) + 1288834974657 // Twitter Snowflake起始时间戳
	machineID := (int64(id) >> 12) & 0x3FF
	sequence := int64(id) & 0xFFF

	return map[string]int64{
		"timestamp": timestamp,
		"machineID": machineID,
		"sequence":  sequence,
	}
}

// GenerateOrderNo 生成带有业务含义的订单号
func GenerateOrderNo(prefix string, snowID snowflake.ID) string {
	// 格式: 业务前缀 + 时间戳(yyyyMMddHHmmss) + 雪花ID后几位
	timestamp := time.Now().Format("20060102150405")
	// 取雪花ID的后6位作为序列号
	sequence := snowID % 1000000
	return fmt.Sprintf("%s%s%06d", prefix, timestamp, sequence)
}
