package logger

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Field type
type Field = zapcore.Field

// CustomHook defines a custom hook function that can access log level, message and fields
type CustomHook func(entry zapcore.Entry, fields []Field) error

// CustomHookWithCtx defines a custom hook function that can access context, log level, message and fields
// This allows hooks to extract request_id, trace_id and other context values
type CustomHookWithCtx func(ctx context.Context, entry zapcore.Entry, fields []Field) error

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

// Skip skips the field
func Skip() Field {
	return zap.Skip()
}

// Err type
func Err(err error) Field {
	if err == nil {
		return zap.Skip()
	}
	return zap.String("err", err.Error())
}

// Any type, if it is a composite type such as object, slice, map, etc., use Any
// For better performance and readability, prefer using specific type functions when possible
func Any(key string, val interface{}) Field {
	return zap.Any(key, val)
}
