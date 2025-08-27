package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Field type
type Field = zapcore.Field

// CustomHook defines a custom hook function that can access log level, message and fields
type CustomHook func(entry zapcore.Entry, fields []Field) error

// Int type
func Int(key string, val int) Field {
	return zap.Int(key, val)
}

// Int32 type
func Int32(key string, val int32) Field {
	return zap.Int32(key, val)
}

// Int64 type
func Int64(key string, val int64) Field {
	return zap.Int64(key, val)
}

// Uint type
func Uint(key string, val uint) Field {
	return zap.Uint(key, val)
}

// Uint32 type
func Uint32(key string, val uint32) Field {
	return zap.Uint32(key, val)
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

	anyToJSON := zapAnyToJSON(val)
	return zap.String(key, anyToJSON)
	//return zap.Any(key, val)
}

func zapAnyToJSON(val interface{}) string {
	if str, ok := val.(string); ok {
		return str
	}
	jsonBytes, err := json.Marshal(val)
	if err != nil {
		return err.Error()
	}
	// 将字节数组转换为字符串并返回
	return string(jsonBytes)
}

func GGetFieldValue(field Field) interface{} {
	switch field.Type {
	case zapcore.StringType:
		return field.String
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
		return field.Integer
	case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
		return field.Integer
	case zapcore.BoolType:
		if b, ok := field.Interface.(bool); ok {
			return b
		}
		return false
	case zapcore.Float64Type:
		if f, ok := field.Interface.(float64); ok {
			return f
		}
		return 0.0
	case zapcore.ErrorType:
		if err, ok := field.Interface.(error); ok {
			return err.Error()
		}
		return ""
	case zapcore.ByteStringType:
		if b, ok := field.Interface.([]byte); ok {
			return string(b)
		}
		return field.Interface
	default:
		// 尝试处理可能的字节切片类型
		if b, ok := field.Interface.([]byte); ok {
			return string(b)
		}
		return field.Interface
	}
}

func ToJSON(fields []zap.Field) string {
	// 创建一个空的 map 用于存储键值对
	keyValuePairs := make(map[string]interface{})

	// 遍历 Zap 字段，将键值对添加到 map 中
	for _, f := range fields {
		key := f.Key

		// 根据字段的类型获取相应的值
		switch f.Type {
		case zapcore.StringType:
			keyValuePairs[key] = f.String
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type, zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			keyValuePairs[key] = strconv.FormatInt(f.Integer, 10)
		case zapcore.Float64Type, zapcore.Float32Type:
			if floatVal, ok := f.Interface.(float64); ok {
				keyValuePairs[key] = strconv.FormatFloat(floatVal, 'f', -1, 64)
			} else if floatVal, ok := f.Interface.(float32); ok {
				keyValuePairs[key] = strconv.FormatFloat(float64(floatVal), 'f', -1, 64)
			}
		case zapcore.BoolType:
			if b, ok := f.Interface.(bool); ok {
				keyValuePairs[key] = strconv.FormatBool(b)
			}
		case zapcore.ByteStringType:
			if bs, ok := f.Interface.([]byte); ok {
				keyValuePairs[key] = string(bs)
			}
		case zapcore.ErrorType:
			if err, ok := f.Interface.(error); ok {
				keyValuePairs[key] = err.Error()
			}
		case zapcore.DurationType:
			if dur, ok := f.Interface.(time.Duration); ok {
				keyValuePairs[key] = dur.String()
			}
		default:
			// 对于其他类型，尝试将其转换为字符串
			keyValuePairs[key] = fmt.Sprintf("%v", f.Interface)
		}
	}
	// 创建一个缓冲区来存储编码后的 JSON 数据
	var buf bytes.Buffer

	// 创建一个 JSON 编码器
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false) // 禁用 HTML 转义

	// 编码 map 为 JSON 字符串
	if err := encoder.Encode(keyValuePairs); err != nil {
		return fmt.Sprintf(`{"error": "%s"}`, err)
	}
	// 获取编码后的 JSON 数据
	jsonData := buf.Bytes()

	// 去除末尾的换行符
	jsonData = bytes.TrimRight(jsonData, "\n")
	return string(jsonData)
}
