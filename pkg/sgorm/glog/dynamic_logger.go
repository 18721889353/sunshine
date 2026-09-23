// Package glog provides a gorm logger implementation based on the project logger.
package glog

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"

	"github.com/18721889353/sunshine/pkg/logger"
)

// dynamicGormLogger 支持动态修改日志开关和慢查询阈值的 GORM logger。
// 用于 Nacos 配置热更新场景，无需重启服务即可调整数据库日志行为。
type dynamicGormLogger struct {
	requestIDKey  string
	logLevel      atomic.Int32 // gormLogger.LogLevel，支持动态切换
	slowThreshold atomic.Int64 // 慢查询阈值（纳秒），0=不启用慢查询日志
	enableLog     atomic.Bool  // 是否启用 SQL 日志
}

// DynamicGormLogger 动态 GORM logger 实例
var DynamicGormLogger = &dynamicGormLogger{
	requestIDKey: string(logger.ContextKeyForRequestID()),
}

// SetLogLevel 动态设置日志级别
func (l *dynamicGormLogger) SetLogLevel(level gormLogger.LogLevel) {
	l.logLevel.Store(int32(level))
}

// SetSlowThreshold 动态设置慢查询阈值
func (l *dynamicGormLogger) SetSlowThreshold(threshold time.Duration) {
	l.slowThreshold.Store(int64(threshold))
}

// SetEnableLog 动态设置是否启用 SQL 日志
func (l *dynamicGormLogger) SetEnableLog(enable bool) {
	l.enableLog.Store(enable)
}

// LogMode 设置日志级别并返回自身（实现 gormLogger.Interface）
func (l *dynamicGormLogger) LogMode(level gormLogger.LogLevel) gormLogger.Interface {
	l.logLevel.Store(int32(level))
	return l
}

// Info 打印 info 级别日志
func (l *dynamicGormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if !l.enableLog.Load() {
		return
	}
	if gormLogger.LogLevel(l.logLevel.Load()) >= gormLogger.Info {
		fields := []logger.Field{
			logger.Any("data", data),
		}
		if v, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && v != "" {
			fields = append(fields, logger.String("request_id", v))
		}
		logger.InfoWithCtx(ctx, msg, fields...)
	}
}

// Warn 打印 warn 级别日志
func (l *dynamicGormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if !l.enableLog.Load() {
		return
	}
	if gormLogger.LogLevel(l.logLevel.Load()) >= gormLogger.Warn {
		fields := []logger.Field{
			logger.Any("data", data),
		}
		if v, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && v != "" {
			fields = append(fields, logger.String("request_id", v))
		}
		logger.WarnWithCtx(ctx, msg, fields...)
	}
}

// Error 打印 error 级别日志
func (l *dynamicGormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if !l.enableLog.Load() {
		return
	}
	if gormLogger.LogLevel(l.logLevel.Load()) >= gormLogger.Error {
		fields := []logger.Field{
			logger.Any("data", data),
		}
		if v, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && v != "" {
			fields = append(fields, logger.String("request_id", v))
		}
		logger.ErrorWithCtx(ctx, msg, fields...)
	}
}

// Trace 打印 SQL 日志，支持慢查询检测
func (l *dynamicGormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	// SQL 日志未启用时直接返回
	if !l.enableLog.Load() {
		return
	}

	level := gormLogger.LogLevel(l.logLevel.Load())
	if level <= gormLogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	var rowsField logger.Field
	if rows == -1 {
		rowsField = logger.String("rows", "-")
	} else {
		rowsField = logger.Int64("rows", rows)
	}

	fields := []logger.Field{
		logger.String("sql", sql),
		rowsField,
		logger.String("ms", fmt.Sprintf("%.2f", float64(elapsed.Nanoseconds())/1e6)),
	}

	if v, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && v != "" {
		fields = append(fields, logger.String("request_id", v))
	}

	// 处理错误
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.DebugWithCtx(ctx, "Gorm query canceled (expected)", fields...)
			return
		}
		logger.ErrorWithCtx(ctx, "Gorm query failed", append(fields, logger.Err(err))...)
		return
	}

	// 检查是否为慢查询
	slowThresholdNs := l.slowThreshold.Load()
	if slowThresholdNs > 0 && elapsed > time.Duration(slowThresholdNs) {
		logger.WarnWithCtx(ctx, "Gorm slow query", fields...)
		return
	}

	// 普通 SQL 日志
	if level >= gormLogger.Info {
		logger.InfoWithCtx(ctx, "Gorm query", fields...)
	} else if level >= gormLogger.Warn {
		logger.WarnWithCtx(ctx, "Gorm query", fields...)
	}
}
