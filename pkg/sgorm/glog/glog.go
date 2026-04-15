// Package glog provides a gorm logger implementation based on the project logger.
package glog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

type gLogger struct {
	requestIDKey string
	logLevel     gormLogger.LogLevel
}

// NewCustomGormLogger custom gorm logger
func NewCustomGormLogger(requestIDKey string, logLevel gormLogger.LogLevel) gormLogger.Interface {
	if requestIDKey == "" {
		requestIDKey = "request_id"
	}
	if logLevel == 0 {
		logLevel = gormLogger.Info
	}
	return &gLogger{
		requestIDKey: requestIDKey,
		logLevel:     logLevel,
	}
}

// LogMode log mode
func (l *gLogger) LogMode(level gormLogger.LogLevel) gormLogger.Interface {
	l.logLevel = level
	return l
}

// Info print info
func (l *gLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= gormLogger.Info {
		msg = strings.ReplaceAll(msg, "%v", "")
		logger.InfoWithCtx(ctx, msg,
			logger.Any("data", data),
			logger.String("line", utils.FileWithLineNum()),
			requestIDField(ctx, l.requestIDKey),
		)
	}
}

// Warn print warn messages
func (l *gLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= gormLogger.Warn {
		msg = strings.ReplaceAll(msg, "%v", "")
		logger.WarnWithCtx(ctx, msg,
			logger.Any("data", data),
			logger.String("line", utils.FileWithLineNum()),
			requestIDField(ctx, l.requestIDKey),
		)
	}
}

// Error print error messages
func (l *gLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= gormLogger.Error {
		msg = strings.ReplaceAll(msg, "%v", "")
		logger.ErrorWithCtx(ctx, msg,
			logger.Any("data", data),
			logger.String("line", utils.FileWithLineNum()),
			requestIDField(ctx, l.requestIDKey),
		)
	}
}

// Trace print sql message
func (l *gLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.logLevel <= gormLogger.Silent {
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

	var fileLineField logger.Field
	fileLine := utils.FileWithLineNum()
	ss := strings.Split(fileLine, "/internal/")
	if len(ss) == 2 {
		fileLineField = logger.String("file_line", ss[1])
	} else {
		fileLineField = logger.String("file_line", fileLine)
	}

	fields := []logger.Field{
		logger.String("sql", sql),
		rowsField,
		logger.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
		fileLineField,
		requestIDField(ctx, l.requestIDKey),
		logger.String("log_from", "sgorm msg Trace"),
	}

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.ErrorWithCtx(ctx, "Gorm msg", append(fields, logger.Err(err))...)
		return
	}

	if l.logLevel >= gormLogger.Info {
		logger.InfoWithCtx(ctx, "Gorm msg", fields...)
		return
	}

	if l.logLevel >= gormLogger.Warn {
		logger.WarnWithCtx(ctx, "Gorm msg", fields...)
	}
}

func requestIDField(ctx context.Context, requestIDKey string) logger.Field {
	if requestIDKey == "" {
		return logger.Skip()
	}
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return logger.String(requestIDKey, v)
	}
	return logger.Skip()
}
