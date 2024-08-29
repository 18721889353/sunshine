package logger

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Field type
type Field = zapcore.Field

// Int type
func Int(key string, val int) Field {
	return zap.Int(key, val)
}

// Int64 type
func Int64(key string, val int64) Field {
	return zap.Int64(key, val)
}

// Uint type
func Uint(key string, val uint) Field {
	return zap.Uint(key, val)
}

// Uint64 type
func Uint64(key string, val uint64) Field {
	return zap.Uint64(key, val)
}

// Uintptr type
func Uintptr(key string, val uintptr) Field {
	return zap.Uintptr(key, val)
}

// Float64 type
func Float64(key string, val float64) Field {
	return zap.Float64(key, val)
}

// Bool type
func Bool(key string, val bool) Field {
	return zap.Bool(key, val)
}

// String type
func String(key string, val string) Field {
	return zap.String(key, val)
}

// Stringer type
func Stringer(key string, val fmt.Stringer) Field {
	return zap.Stringer(key, val)
}

// Time type
func Time(key string, val time.Time) Field {
	return zap.Time(key, val)
}

// Duration type
func Duration(key string, val time.Duration) Field {
	return zap.Duration(key, val)
}

// Err type
func Err(err error) Field {
	return zap.String("err", err.Error())
	//return zap.Error(err)
}

// Any type, if it is a composite type such as object, slice, map, etc., use Any
func Any(key string, val interface{}) Field {

	anyToJSON := zapAnyToJSON(key, val)
	return zap.String(key, anyToJSON)
	//return zap.Any(key, val)
}

func zapAnyToJSON(key string, val interface{}) string {
	// 创建一个空的 map 用于存储键值对
	data := make(map[string]interface{})
	// 将 zap.Any 的键值对添加到 map 中
	data[key] = val
	// 将 map 转换为 JSON 格式的字节数组
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err.Error()
	}
	// 将字节数组转换为字符串并返回
	return string(jsonBytes)
}
