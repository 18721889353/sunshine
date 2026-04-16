package goredis

import (
	"context"
	"fmt"
	"strings"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// requestIDHook 自定义 Redis Hook，用于从 Context 提取 request_id 并设置到 Span 属性
type requestIDHook struct{}

// DialHook 实现 redis.DialHook 接口
func (h *requestIDHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

// ProcessHook 实现 redis.ProcessHook 接口
func (h *requestIDHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		// 此时 redisotel (外层 Hook) 已创建 Redis Span 并注入 Context
		// 增强当前 Redis Span 的诊断属性
		enhanceRedisSpan(ctx, cmd)

		// 执行底层 Redis 命令
		err := next(ctx, cmd)

		// 大厂标准：记录错误到 Span（关键！）
		if err != nil {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.RecordError(err,
					trace.WithAttributes(
						attribute.String("error.type", fmt.Sprintf("%T", err)),
						attribute.String("error.context", "redis-command-failed"),
						attribute.String("redis.command", cmd.Name()),
					),
				)
				span.SetStatus(codes.Error, fmt.Sprintf("redis %s failed: %v", cmd.Name(), err))
			}
		}

		return err
	}
}

// ProcessPipelineHook 实现 redis.ProcessPipelineHook 接口
func (h *requestIDHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		// 此时 redisotel (外层 Hook) 已创建 Pipeline Span
		// 直接设置 request_id 到当前的 Pipeline Span
		setRequestIDToRedisSpan(ctx)

		// 执行底层 Pipeline 命令
		return next(ctx, cmds)
	}
}

// enhanceRedisSpan 增强 Redis Span 信息（大厂标准：添加 Key、耗时等诊断属性）
func enhanceRedisSpan(ctx context.Context, cmd redis.Cmder) {
	// 1. 提取 request_id
	setRequestIDToRedisSpan(ctx)

	// 2. 获取当前 Span
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	commandName := cmd.Name()
	// 3. 统一 Span 名称为 redis.{command} 格式（大厂标准规范）
	// 特殊处理 evalsha：根据 Key 前缀自动识别分布式锁操作
	if commandName == "evalsha" && len(cmd.Args()) > 2 {
		// evalsha SHA1 numkeys key [key ...] arg [arg ...]
		// 提取第一个 Key（参数索引为 2）
		keyStr := fmt.Sprintf("%v", cmd.Args()[2])

		// 根据 Key 前缀识别分布式锁操作（大厂标准）
		if strings.Contains(keyStr, "lock:") || strings.Contains(keyStr, "/dlock/") {
			// 提取锁名称（去掉前缀）
			lockName := keyStr
			if idx := strings.LastIndex(lockName, ":"); idx != -1 {
				lockName = lockName[idx+1:]
			} else if idx := strings.LastIndex(lockName, "/"); idx != -1 {
				lockName = lockName[idx+1:]
			}
			// 截取前 20 个字符
			if len(lockName) > 20 {
				lockName = lockName[:20]
			}
			span.SetName("redis.lock:" + lockName)
		} else {
			// 其他 evalsha 显示 SHA1 前 8 位
			sha1 := fmt.Sprintf("%v", cmd.Args()[1])
			if len(sha1) > 8 {
				sha1 = sha1[:8]
			}
			span.SetName("redis.evalsha:" + sha1)
		}
	} else {
		span.SetName("redis." + commandName)
	}

	// 4. 添加 Redis 诊断属性
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.redis.command", commandName),
	)

	// 5. 提取 Key（截取前 100 个字符，避免过长）
	keys := cmd.Args()
	if len(keys) > 1 {
		// 跳过 evalsha 的第一个参数（SHA1 哈希值），从第二个参数开始取 Key
		keyIndex := 1
		if commandName == "evalsha" {
			keyIndex = 2 // evalsha SHA1 numkeys key [key ...] arg [arg ...]
		}
		if len(keys) > keyIndex {
			keyStr := fmt.Sprintf("%v", keys[keyIndex])
			if len(keyStr) > 100 {
				keyStr = keyStr[:100] + "..."
			}
			span.SetAttributes(attribute.String("db.redis.key", keyStr))
		}
	}

	// 6. 添加事件标记
	span.AddEvent("redis command executing",
		trace.WithAttributes(
			attribute.String("redis.command", commandName),
			attribute.Int("redis.args_count", len(keys)-1),
		))
}

// setRequestIDToRedisSpan 从 Context 提取 request_id 并设置到当前 Span 属性
func setRequestIDToRedisSpan(ctx context.Context) {
	// 从 Context 中提取 request_id（使用 logger 统一的常量）
	if reqID := ctx.Value(logger.ContextKeyRequestID); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			// 获取当前 Span 并设置属性
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.SetAttributes(attribute.String(string(logger.ContextKeyForRequestID()), reqIDStr))
			}
		}
	}
}
