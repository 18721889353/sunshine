package ggorm

import (
	"context"
	"fmt"
	"strings"
	"time"

	pkgLogger "github.com/18721889353/sunshine/pkg/logger"
	"go.uber.org/zap"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

type gormLogger struct {
	gLog         *zap.Logger
	requestIDKey string
	logLevel     logger.LogLevel
}

// NewCustomGormLogger custom gorm logger
func NewCustomGormLogger(o *options) logger.Interface {
	return &gormLogger{
		gLog:         o.gLog,
		requestIDKey: o.requestIDKey,
		logLevel:     o.logLevel,
	}
}

// LogMode log mode
func (l *gormLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.logLevel = level
	return l
}

// Info print info
func (l *gormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= logger.Info {
		msg = strings.ReplaceAll(msg, "%v", "")
		pkgLogger.Info(msg, zap.Any("data", data), zap.String("line", utils.FileWithLineNum()), requestIDField(ctx, l.requestIDKey))
	}
}

// Warn print warn messages
func (l *gormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= logger.Warn {
		msg = strings.ReplaceAll(msg, "%v", "")
		pkgLogger.Warn(msg, zap.Any("data", data), zap.String("line", utils.FileWithLineNum()), requestIDField(ctx, l.requestIDKey))
	}
}

// Error print error messages
func (l *gormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= logger.Error {
		msg = strings.ReplaceAll(msg, "%v", "")
		pkgLogger.Warn(msg, zap.Any("data", data), zap.String("line", utils.FileWithLineNum()), requestIDField(ctx, l.requestIDKey))
	}
}

// Trace print sql message
func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.logLevel <= logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	var rowsField zap.Field
	if rows == -1 {
		rowsField = zap.String("rows", "-")
	} else {
		rowsField = zap.Int64("rows", rows)
	}

	var fileLineField zap.Field
	fileLine := utils.FileWithLineNum()
	ss := strings.Split(fileLine, "/internal/")
	if len(ss) == 2 {
		fileLineField = zap.String("file_line", ss[1])
	} else {
		fileLineField = zap.String("file_line", fileLine)
	}

	if err != nil {
		pkgLogger.Warn("Gorm msg",
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.Error(err),
			zap.String("sql", sql),
			rowsField,
			zap.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
			fileLineField,
			requestIDField(ctx, l.requestIDKey),
			zap.String("log_from", "Gorm msg Trace"),
		)
		return
	}

	if l.logLevel >= logger.Info {
		pkgLogger.Info("Gorm msg",
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("sql", sql),
			rowsField,
			zap.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
			fileLineField,
			requestIDField(ctx, l.requestIDKey),
			zap.String("log_from", "Gorm msg Trace"),
		)
		return
	}

	if l.logLevel >= logger.Warn {
		pkgLogger.Warn("Gorm msg",
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("sql", sql),
			rowsField,
			zap.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
			fileLineField,
			requestIDField(ctx, l.requestIDKey),
			zap.String("log_from", "Gorm msg Trace"),
		)
	}
}

func requestIDField(ctx context.Context, requestIDKey string) zap.Field {
	if requestIDKey == "" {
		return zap.Skip()
	}

	var field zap.Field
	if requestIDKey != "" {
		if v, ok := ctx.Value(requestIDKey).(string); ok {
			field = zap.String(requestIDKey, v)
		} else {
			field = zap.Skip()
		}
	}
	return field
}
