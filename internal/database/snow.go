// Package database 提供数据库客户端初始化及相关基础设施（如雪花ID生成器）。
package database

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"sync"
	"time"

	"github.com/bwmarrin/snowflake"

	"github.com/18721889353/sunshine/pkg/logger"
)

// snowflakeEpoch 是 Twitter Snowflake 的起始时间戳（2010-11-04 01:42:54 UTC）。
// 雪花ID的时间戳部分基于此偏移量计算实际时间。
const snowflakeEpoch = 1288834974657

var (
	// snowNode 是全局雪花算法节点实例，通过 sync.Once 保证只初始化一次。
	snowNode *snowflake.Node
	snowOnce sync.Once
)

// GetSnowNode 获取全局雪花算法节点实例，按需初始化。
// 首次调用时会自动执行 InitSnowNode 完成初始化，后续调用直接返回缓存实例。
func GetSnowNode() *snowflake.Node {
	if snowNode == nil {
		snowOnce.Do(func() {
			InitSnowNode()
		})
	}
	return snowNode
}

// InitSnowNode 初始化雪花算法节点。
// 从本机IP计算唯一机器ID（0~1023），创建 snowflake.Node 并缓存到全局变量 snowNode 中。
// 初始化失败时会 panic（如无法获取本机有效IP地址）。
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

// getMachineID 计算本机雪花算法节点ID。
// 通过本机非回环 IPv4 地址哈希映射到 [0, 1024) 范围，确保分布式环境下每台机器ID唯一。
func getMachineID() (int64, error) {
	machineID, err := calculateMachineIDFromIP()
	if err != nil {
		return 0, fmt.Errorf("failed to calculate machine ID from IP: %w", err)
	}
	// 确保MachineID在合法范围内(0-1023)
	return int64(machineID % 1024), nil
}

// calculateMachineIDFromIP 遍历本机网络接口，取第一个非回环 IPv4 地址进行 FNV32a 哈希，
// 映射到雪花算法合法的机器ID范围 [0, 1024)。
func calculateMachineIDFromIP() (int, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return 0, fmt.Errorf("failed to get interface addresses: %w", err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		// 对本机IP进行哈希，映射到雪花算法合法的机器ID范围(0-1023)
		hash := fnv.New32a()
		hash.Write([]byte(ipNet.IP.String()))
		return int(hash.Sum32()) % 1024, nil
	}
	return 0, errors.New("failed to get interface addresses")
}

// GetSnowID 生成并返回一个新的雪花ID，作为全局唯一分布式ID。
func GetSnowID() snowflake.ID {
	return snowNode.Generate()
}

// extractSnowflakeTimestamp 从雪花ID中提取时间戳（毫秒），包含Twitter Snowflake起始偏移。
// 雪花ID结构：时间戳(41bit) | 机器ID(10bit) | 序列号(12bit)，右移22位获取时间戳。
func extractSnowflakeTimestamp(id snowflake.ID) int64 {
	return (int64(id) >> 22) + snowflakeEpoch
}

// GetTimeFromSnowID 从雪花ID中解析出生成时间。
func GetTimeFromSnowID(id snowflake.ID) time.Time {
	timestamp := extractSnowflakeTimestamp(id)
	return time.Unix(0, timestamp*int64(time.Millisecond))
}

// GetSequenceFromSnowID 从雪花ID中提取序列号
func GetSequenceFromSnowID(id snowflake.ID) int64 {
	// 序列号是雪花ID的最低12位
	sequence := int64(id) & 0xFFF
	return sequence
}

// ParseSnowID 解析雪花ID的各个组成部分：timestamp（毫秒）、machineID、sequence。
func ParseSnowID(id snowflake.ID) map[string]int64 {
	timestamp := extractSnowflakeTimestamp(id)
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

const (
	// SnowflakeIDLength 雪花算法生成的ID长度（19位十进制数字）
	SnowflakeIDLength = 19
)

// IsSnowflakeID 判断字符串是否是雪花算法生成的ID（19位纯数字）
// 用于区分 Go Producer（雪花ID）和其他语言/系统（UUID或其他格式）发送的消息
// 注意：此函数仅做格式校验，不做数值范围校验
func IsSnowflakeID(id string) bool {
	if len(id) != SnowflakeIDLength {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
