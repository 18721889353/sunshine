// Package glog provides a gorm logger implementation based on the project logger.
package glog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gorm.io/gorm/utils"

	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"

	"github.com/18721889353/sunshine/pkg/logger"
)

type gLogger struct {
	requestIDKey string
	logLevel     gormLogger.LogLevel
}

// NewCustomGormLogger custom gorm logger
func NewCustomGormLogger(requestIDKey string, logLevel gormLogger.LogLevel) gormLogger.Interface {
	if requestIDKey == "" {
		requestIDKey = string(logger.ContextKeyForRequestID())
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
		fields := []logger.Field{
			logger.Any("data", data),
			logger.String("line", utils.FileWithLineNum()),
		}
		if v, ok := ctx.Value(l.requestIDKey).(string); ok && v != "" {
			fields = append(fields, logger.String(l.requestIDKey, v))
		}
		logger.InfoWithCtx(ctx, msg, fields...)
	}
}

// Warn print warn messages
func (l *gLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= gormLogger.Warn {
		msg = strings.ReplaceAll(msg, "%v", "")
		fields := []logger.Field{
			logger.Any("data", data),
			logger.String("line", utils.FileWithLineNum()),
		}
		if v, ok := ctx.Value(l.requestIDKey).(string); ok && v != "" {
			fields = append(fields, logger.String(l.requestIDKey, v))
		}
		logger.WarnWithCtx(ctx, msg, fields...)
	}
}

// Error print error messages
func (l *gLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.logLevel >= gormLogger.Error {
		msg = strings.ReplaceAll(msg, "%v", "")
		fields := []logger.Field{
			logger.Any("data", data),
			logger.String("line", utils.FileWithLineNum()),
		}
		if v, ok := ctx.Value(l.requestIDKey).(string); ok && v != "" {
			fields = append(fields, logger.String(l.requestIDKey, v))
		}
		logger.ErrorWithCtx(ctx, msg, fields...)
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

	// 构建基础字段
	fields := []logger.Field{
		logger.String("sql", sql),
		rowsField,
		logger.String("ms", fmt.Sprintf("%v", float64(elapsed.Nanoseconds())/1e6)),
	}

	// 按需添加 request_id 字段
	if v, ok := ctx.Value(l.requestIDKey).(string); ok && v != "" {
		fields = append(fields, logger.String(l.requestIDKey, v))
	}

	// 获取上游调用者位置（业务代码调用 GORM 的位置）
	// skip=1 配合调用栈过滤，定位到业务代码
	// 只有当获取到有效位置时才添加 file_line 字段，避免产生空 key/value
	if callerFile, callerLine := getCallerFileLine(1); callerFile != "" && callerLine > 0 {
		// 将 file_line 插入到最前面，便于查看
		fields = append([]logger.Field{logger.String("file_line", formatFileLine(callerFile, callerLine))}, fields...)
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

// getCallerFileLine 获取业务代码的调用者位置（优先返回最外层）
// skip: 从当前函数开始跳过的栈帧数
func getCallerFileLine(skip int) (string, int) {
	// 获取调用栈（最多30层，避免极端情况性能问题）
	var pcs [30]uintptr
	n := runtime.Callers(skip, pcs[:])
	if n == 0 {
		return "", 0
	}

	frames := runtime.CallersFrames(pcs[:n])
	var firstBusinessFrame *runtime.Frame
	maxFrames := 20 // 最多遍历20个业务帧，避免性能问题
	frameCount := 0

	for {
		frame, more := frames.Next()
		frameCount++

		// 超过最大遍历次数，返回第一个业务帧（兜底）
		if frameCount > maxFrames && firstBusinessFrame != nil {
			return firstBusinessFrame.File, firstBusinessFrame.Line
		}

		file := frame.File

		// 快速跳过框架代码（使用 Contains 兼容 Module 缓存路径带版本号的情况）
		if strings.Contains(file, "gorm") ||
			strings.Contains(file, "runtime") ||
			strings.Contains(file, "sunshine/pkg/logger") ||
			strings.Contains(file, "sunshine/pkg/sgorm/glog") ||
			strings.Contains(file, "singleflight") ||
			strings.Contains(file, "golang.org/x/sync") ||
			strings.Contains(file, "sync/") {
			if !more {
				break
			}
			continue
		}

		// 记录第一个业务代码帧（可能是 DAO 层）
		if firstBusinessFrame == nil {
			f := frame // 深拷贝 frame，因为 Next() 会复用内存
			firstBusinessFrame = &f
		}

		// 如果是 DAO 层，继续向上找更外层调用者
		if strings.Contains(file, "/internal/dao/") {
			if !more {
				break
			}
			continue
		}

		// 如果是 Handler/Service/Consumer 层，直接返回（更上层）
		return frame.File, frame.Line
	}

	// 兜底：如果整个调用栈都是 DAO 层（或没有其他业务层），返回第一层业务代码
	if firstBusinessFrame != nil {
		if firstBusinessFrame.File != "" && firstBusinessFrame.Line > 0 {
			return firstBusinessFrame.File, firstBusinessFrame.Line
		}
	}

	return "", 0
}

// formatFileLine 格式化文件路径和行号
func formatFileLine(file string, line int) string {
	// 移除 Windows 路径前缀
	file = strings.ReplaceAll(file, "\\", "/")

	// 尝试提取项目相对路径
	if idx := strings.Index(file, "/src/"); idx != -1 {
		return file[idx+1:] + ":" + fmt.Sprintf("%d", line)
	}

	// 如果没有 src，尝试提取最后两层目录
	if idx := strings.LastIndex(file, "/"); idx != -1 {
		dir := file[:idx]
		if idx2 := strings.LastIndex(dir, "/"); idx2 != -1 {
			return dir[idx2+1:] + "/" + filepath.Base(file) + ":" + fmt.Sprintf("%d", line)
		}
	}

	// 兜底：返回完整路径
	return file + ":" + fmt.Sprintf("%d", line)
}
