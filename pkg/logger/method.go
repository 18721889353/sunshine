package logger

import (
	"encoding/json"
	"strings"

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

	//// 创建一个空的 map 用于存储键值对
	//keyValuePairs := make(map[string]interface{})
	//// 遍历 Zap 字段，将键值对添加到 map 中
	//for _, f := range fields {
	//	key := f.Key
	//	// 根据字段的类型获取相应的值
	//	switch f.Type {
	//	case zapcore.StringType:
	//		keyValuePairs[key] = f.String
	//	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type, zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
	//		keyValuePairs[key] = f.Integer
	//	default:
	//		continue
	//	}
	//}
	// 将 []zap.Field 序列化为 JSON 字符串
	jsonData, err := json.Marshal(fields)
	if err != nil {
		return ""
	}
	jsonString := string(jsonData)
	return jsonString
}
