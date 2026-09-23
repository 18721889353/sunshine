package logger

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Field 日志字段类型
type Field = zapcore.Field

// CustomHook 自定义钩子函数，可访问日志级别、消息和字段
type CustomHook func(entry zapcore.Entry, fields []Field) error

// CustomHookWithCtx 带 Context 的自定义钩子函数，可访问上下文、日志级别、消息和字段
// 钩子可从 context 中提取 request_id、trace_id 等链路追踪信息
type CustomHookWithCtx func(ctx context.Context, entry zapcore.Entry, fields []Field) error

// Int 创建整数类型字段
func Int(key string, val int) Field {
	return zap.Int(key, val)
}

// Int32 创建 Int32 类型字段
func Int32(key string, val int32) Field {
	return zap.Int32(key, val)
}

// Int64 创建 Int64 类型字段
func Int64(key string, val int64) Field {
	return zap.Int64(key, val)
}

// Uint 创建无符号整数类型字段
func Uint(key string, val uint) Field {
	return zap.Uint(key, val)
}

// Uint32 创建 Uint32 类型字段
func Uint32(key string, val uint32) Field {
	return zap.Uint32(key, val)
}

// Uint64 创建 Uint64 类型字段
func Uint64(key string, val uint64) Field {
	return zap.Uint64(key, val)
}

// Uintptr 创建指针大小整数类型字段
func Uintptr(key string, val uintptr) Field {
	return zap.Uintptr(key, val)
}

// Float64 创建浮点数类型字段
func Float64(key string, val float64) Field {
	return zap.Float64(key, val)
}

// Bool 创建布尔类型字段
func Bool(key string, val bool) Field {
	return zap.Bool(key, val)
}

// String 创建字符串类型字段
func String(key string, val string) Field {
	return zap.String(key, val)
}

// Stringer 创建 Stringer 类型字段
func Stringer(key string, val fmt.Stringer) Field {
	return zap.Stringer(key, val)
}

// Time 创建时间类型字段
func Time(key string, val time.Time) Field {
	return zap.Time(key, val)
}

// Duration 创建时间间隔类型字段
func Duration(key string, val time.Duration) Field {
	return zap.Duration(key, val)
}

// Skip 跳过该字段
func Skip() Field {
	return zap.Skip()
}

// Err 创建错误类型字段，err 为 nil 时自动跳过
func Err(err error) Field {
	if err == nil {
		return zap.Skip()
	}
	return zap.Error(err)
}

// Any 创建任意类型字段，适用于对象、切片、映射等复合类型
// 为了更好的性能和可读性，优先使用具体的类型函数
func Any(key string, val interface{}) Field {
	return zap.Any(key, val)
}
