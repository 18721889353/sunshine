package logger

import (
	"encoding/json"
	"fmt"
	"go.uber.org/zap/zapcore"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Debug level information
func Debug(msg string, fields ...Field) {
	getLogger().Debug(msg, fields...)
}

// Info level information
func Info(msg string, fields ...Field) {
	//getLogger().Info(msg, fields...)
	fields = append(fields, zap.String("log_msg", msg))
	getLogger().Info(toJSON(fields))
}

// Warn level information
func Warn(msg string, fields ...Field) {
	//getLogger().Warn(msg, fields...)
	fields = append(fields, zap.String("log_msg", msg))
	getLogger().Warn(toJSON(fields))
}

// Error level information
func Error(msg string, fields ...Field) {
	//getLogger().Error(msg, fields...)
	fields = append(fields, zap.String("log_msg", msg))
	getLogger().Error(toJSON(fields))

}

// Panic level information
func Panic(msg string, fields ...Field) {
	//getLogger().Panic(msg, fields...)
	fields = append(fields, zap.String("log_msg", msg))
	getLogger().Panic(toJSON(fields))
}

// Fatal level information
func Fatal(msg string, fields ...Field) {
	//getLogger().Fatal(msg, fields...)
	fields = append(fields, zap.String("log_msg", msg))
	getLogger().Fatal(toJSON(fields))
}

// Debugf format level information
func Debugf(format string, a ...interface{}) {
	getSugaredLogger().Debugf(format, a...)
}

// Infof format level information
func Infof(format string, a ...interface{}) {
	getSugaredLogger().Infof(format, a...)
}

// Warnf format level information
func Warnf(format string, a ...interface{}) {
	getSugaredLogger().Warnf(format, a...)
}

// Errorf format level information
func Errorf(format string, a ...interface{}) {
	getSugaredLogger().Errorf(format, a...)
}

// Fatalf format level information
func Fatalf(format string, a ...interface{}) {
	getSugaredLogger().Fatalf(format, a...)
}

// Sync flushing any buffered log entries, applications should take care to call Sync before exiting.
func Sync() error {
	_ = getSugaredLogger().Sync()
	err := getLogger().Sync()
	if err != nil && !strings.Contains(err.Error(), "/dev/stdout") {
		return err
	}
	return nil
}

// WithFields carrying field information
func WithFields(fields ...Field) *zap.Logger {
	return GetWithSkip(0).With(fields...)
}
func toJSON(fields []zap.Field) string {
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

	// 将 map 转换为 JSON 格式的字符串
	jsonBytes, err := json.Marshal(keyValuePairs)
	if err != nil {
		return fmt.Sprintf(`{"error": "%s"}`, err)
	}
	return string(jsonBytes)
}
