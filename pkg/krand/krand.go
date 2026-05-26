// Package krand 提供生成随机字符串、整数和浮点数的工具。
// 支持多种字符集和范围，线程安全。
package krand

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

// 随机字符串生成的字符类型常量。
const (
	RNum   = 1 << iota // RNum 表示数字字符 (0-9)
	RUpper             // RUpper 表示大写字母 (A-Z)
	RLower             // RLower 表示小写字母 (a-z)
	RAll               // RAll 表示所有字符类型（数字 + 大写 + 小写）
)

var (
	// refChars 包含随机字符串生成的所有可用字符
	refChars = []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")

	// charSets 将字符类型标志映射到对应的字符切片
	charSets = map[int][]byte{
		RNum:            refChars[0:10],  // 0-9
		RUpper:          refChars[10:36], // A-Z
		RNum | RUpper:   refChars[0:36],  // 0-9 + A-Z
		RLower:          refChars[36:62], // a-z
		RNum | RLower:   refChars[0:10],  // 这将在代码中特殊处理
		RUpper | RLower: refChars[10:62], // A-Z + a-z
		RAll:            refChars,        // 所有字符
	}
)

// String 生成指定长度和字符类型的密码学安全随机字符串。
// 如果未提供 size 参数，默认为 6 个字符。
//
// 参数:
//   - kind: 字符类型标志（RNum、RUpper、RLower、RAll 或组合）
//   - size: 可选的生成字符串长度（默认：6）
//
// 返回:
//   - string: 随机生成的字符串
//
// 示例:
//
//	String(RAll)           // "aB3kL9" (6个字符，所有类型)
//	String(RAll, 16)       // "aB3kL9mN2pQ5rT8w" (16个字符)
//	String(RNum|RLower, 8) // "3k7m9p2q" (8个字符，数字 + 小写)
func String(kind int, size ...int) string {
	return string(Bytes(kind, size...))
}

// Bytes 生成指定长度和字符类型的密码学安全随机字节切片。
// 如果未提供 bytesLen 参数，默认为 6 个字节。
//
// 参数:
//   - kind: 字符类型标志（RNum、RUpper、RLower、RAll 或组合）
//   - bytesLen: 可选的生成字节切片长度（默认：6）
//
// 返回:
//   - []byte: 随机生成的字节切片
//
// 示例:
//
//	Bytes(RAll)           // []byte("aB3kL9")
//	Bytes(RAll, 16)       // []byte("aB3kL9mN2pQ5rT8w")
//	Bytes(RNum|RLower, 8) // []byte("3k7m9p2q")
func Bytes(kind int, bytesLen ...int) []byte {
	// 验证并规范化 kind 参数
	if kind <= 0 || kind > RAll {
		kind = RAll
	}

	// 确定长度（默认：6）
	length := 6
	if len(bytesLen) > 0 && bytesLen[0] > 0 {
		length = bytesLen[0]
	}

	// 获取指定 kind 的字符集
	charSet := getCharSet(kind)

	// 使用 crypto/rand 生成随机字节
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charSet))))
		if err != nil {
			// 如果 crypto/rand 失败，回退到安全值（极罕见）
			result[i] = charSet[0]
			continue
		}
		result[i] = charSet[idx.Int64()]
	}

	return result
}

// Int 生成指定范围内的密码学安全随机整数。
// 支持多种调用方式：Int()、Int(max)、Int(min, max)。
// 范围包含两端：[min, max]。
//
// 参数:
//   - rangeSize: 可选的范围参数
//   - 无参数：返回 [0, 100] 范围内的随机整数
//   - 一个参数 (max)：返回 [0, max] 范围内的随机整数
//   - 两个参数 (min, max)：返回 [min, max] 范围内的随机整数（顺序无关）
//
// 返回:
//   - int: 随机生成的整数
//
// 示例:
//
//	Int()           // [0, 100] 范围内的随机整数
//	Int(200)        // [0, 200] 范围内的随机整数
//	Int(1000, 2000) // [1000, 2000] 范围内的随机整数
//	Int(2000, 1000) // [1000, 2000] 范围内的随机整数（自动交换）
func Int(rangeSize ...int) int {
	switch len(rangeSize) {
	case 0:
		// 默认范围：[0, 100]
		return randIntRange(0, 100)
	case 1:
		// 范围：[0, max]
		if rangeSize[0] < 0 {
			return 0
		}
		return randIntRange(0, rangeSize[0])
	default:
		// 范围：[minValue, maxValue]（如需要自动交换）
		minValue, maxValue := rangeSize[0], rangeSize[1]
		if minValue > maxValue {
			minValue, maxValue = maxValue, minValue
		}
		return randIntRange(minValue, maxValue)
	}
}

// Float64 生成指定小数精度的密码学安全随机浮点数。
// 支持多种调用方式以适应不同范围。
//
// 参数:
//   - dpLength: 小数位数（建议 0-10）
//   - rangeSize: 可选的范围参数
//   - 无参数：返回 [0.0, 100.0] 范围内的浮点数
//   - 一个参数 (max)：返回 [0.0, float64(max)] 范围内的浮点数
//   - 两个参数 (min, max)：返回 [float64(min), float64(max)] 范围内的浮点数
//
// 返回:
//   - float64: 随机生成的浮点数
//
// 示例:
//
//	Float64(1, 200)       // [0.0, 200.0] 范围内，1位小数的随机浮点数
//	Float64(2, 100, 1000) // [100.00, 1000.00] 范围内，2位小数的随机浮点数
func Float64(dpLength int, rangeSize ...int) float64 {
	// 根据范围参数确定最小值和最大值
	var minVal, maxVal int
	switch len(rangeSize) {
	case 0:
		// 默认范围：[0, 100]
		minVal, maxVal = 0, 100
	case 1:
		// 范围：[0, max]
		if rangeSize[0] < 0 {
			return 0.0
		}
		minVal, maxVal = 0, rangeSize[0]
	default:
		// 范围：[minVal, maxVal]
		minVal, maxVal = rangeSize[0], rangeSize[1]
		if minVal > maxVal {
			minVal, maxVal = maxVal, minVal
		}
	}

	// 在 [minVal, maxVal] 范围内生成整数部分
	intPart := randIntRange(minVal, maxVal)

	// 计算小数部分
	var decimalPart float64
	if dpLength > 0 {
		// 限制小数位数在合理范围内
		if dpLength > 10 {
			dpLength = 10
		}
		divisor := 1
		for i := 0; i < dpLength; i++ {
			divisor *= 10
		}
		decimalPart = float64(randIntRange(0, divisor-1)) / float64(divisor)
	}

	// 组合整数和小数部分
	result := float64(intPart) + decimalPart

	// 确保结果不因小数加法而超过 maxVal
	if result > float64(maxVal) {
		result = float64(maxVal)
	}

	return result
}

// NewID 基于当前时间戳（毫秒）加随机数生成唯一 ID。
// ID 长度为 19 位数字，适合用作数据库主键。
// 注意：在极高并发场景下，同一毫秒内可能会产生重复。
// 如需更好的唯一性保证，请使用 NewSeriesID()。
//
// 返回:
//   - int64: 唯一 ID，格式：timestamp_ms * 1000000 + random(0-999999)
//
// 示例:
//
//	NewID() // 1701234567890397409
func NewID() int64 {
	timestampMs := time.Now().UnixMilli()
	randomPart, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		// 如果 crypto/rand 失败，仅使用时间戳（极罕见）
		return timestampMs * 1000000
	}
	return timestampMs*1000000 + randomPart.Int64()
}

// NewStringID 生成 NewID() 的十六进制字符串表示形式。
// 生成的字符串长度固定为 16 个字符。
// 在高并发场景下，如需更好的唯一性，请考虑使用 NewSeriesID()。
//
// 返回:
//   - string: 十六进制 ID 字符串
//
// 示例:
//
//	NewStringID() // "179bffd372b8e8e1"
func NewStringID() string {
	return fmt.Sprintf("%016x", NewID())
}

// NewSeriesID 生成可读的系列 ID，包含微秒级精度时间戳。
// 格式：YYYYMMDDHHmmssffffffRRRRRR（共 26 个字符）
//   - 前 20 个字符：微秒级时间戳（YYYYMMDDHHmmssffffff）
//   - 后 6 个字符：随机数字字符串
//
// 返回:
//   - string: 系列 ID 字符串
//
// 示例:
//
//	NewSeriesID() // "20240102150405123456789012"
func NewSeriesID() string {
	now := time.Now()
	// 格式：YYYYMMDDHHmmssffffff（20个字符，不含点号）
	timestamp := now.Format("20060102150405") + fmt.Sprintf("%06d", now.Nanosecond()/1000)
	randomPart := String(RNum, 6)
	return timestamp + randomPart
}

// ==================== 辅助函数 ====================

// getCharSet 返回指定 kind 标志对应的字符集。
// 它处理 RNum、RUpper 和 RLower 的所有有效组合。
func getCharSet(kind int) []byte {
	// 处理特殊情况：RNum | RLower（应该是数字 + 小写）
	if kind == (RNum | RLower) {
		// 组合数字 (0-9) 和小写字母 (a-z)
		result := make([]byte, 0, 36)
		result = append(result, refChars[0:10]...)  // 0-9
		result = append(result, refChars[36:62]...) // a-z
		return result
	}

	// 尝试在 charSets 映射中查找精确匹配
	if charSet, ok := charSets[kind]; ok {
		return charSet
	}

	// 如果未找到匹配，回退到 RAll
	return refChars
}

// randIntRange 生成 [minValue, maxValue] 范围内的密码学安全随机整数。
// minValue 和 maxValue 都包含在内。
func randIntRange(minValue, maxValue int) int {
	if minValue > maxValue {
		minValue, maxValue = maxValue, minValue
	}

	rangeSize := maxValue - minValue + 1
	if rangeSize <= 0 {
		return minValue
	}

	randomBig, err := rand.Int(rand.Reader, big.NewInt(int64(rangeSize)))
	if err != nil {
		// 极罕见的回退
		return minValue
	}

	return minValue + int(randomBig.Int64())
}
