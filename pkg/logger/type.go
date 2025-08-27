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
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
			keyValuePairs[key] = f.Integer
		case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			keyValuePairs[key] = f.Integer
		case zapcore.BoolType:
			if b, ok := f.Interface.(bool); ok {
				keyValuePairs[key] = b
			} else {
				keyValuePairs[key] = false
			}
		case zapcore.Float64Type:
			if fl, ok := f.Interface.(float64); ok {
				keyValuePairs[key] = fl
			} else {
				keyValuePairs[key] = 0.0
			}
		case zapcore.Float32Type:
			if fl, ok := f.Interface.(float32); ok {
				keyValuePairs[key] = fl
			} else {
				keyValuePairs[key] = float32(0.0)
			}
		case zapcore.ErrorType:
			if err, ok := f.Interface.(error); ok {
				keyValuePairs[key] = err.Error()
			} else {
				keyValuePairs[key] = ""
			}
		case zapcore.StringerType:
			if str, ok := f.Interface.(fmt.Stringer); ok {
				keyValuePairs[key] = str.String()
			} else {
				keyValuePairs[key] = fmt.Sprintf("%v", f.Interface)
			}
		case zapcore.DurationType:
			if d, ok := f.Interface.(time.Duration); ok {
				keyValuePairs[key] = d.String()
			} else {
				keyValuePairs[key] = time.Duration(f.Integer).String()
			}
		case zapcore.TimeType:
			if t, ok := f.Interface.(time.Time); ok {
				keyValuePairs[key] = t.Format("2006-01-02 15:04:05")
			} else {
				keyValuePairs[key] = time.Unix(0, f.Integer).Format("2006-01-02 15:04:05")
			}
		case zapcore.ByteStringType:
			if b, ok := f.Interface.([]byte); ok {
				keyValuePairs[key] = string(b)
			} else {
				keyValuePairs[key] = f.Interface
			}
		case zapcore.ReflectType:
			// 使用反射处理复杂类型
			keyValuePairs[key] = fmt.Sprintf("%+v", f.Interface)
		case zapcore.SkipType:
			// 跳过的字段不处理
			continue
		default:
			// 尝试处理可能的字节切片类型或其他类型
			if b, ok := f.Interface.([]byte); ok {
				keyValuePairs[key] = string(b)
			} else {
				keyValuePairs[key] = f.Interface
			}
		}
	}
	// 将 map 转换为 JSON 格式的字符串
	jsonBytes, err := json.Marshal(keyValuePairs)
	if err != nil {
		return fmt.Sprintf(`{"error": "%s"}`, err)
	}
	return string(jsonBytes)
}
