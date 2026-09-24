package goredis

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// requestIDHook 自定义 Redis Hook，用于从 Context 提取 request_id 并设置到 Span 属性
type requestIDHook struct {
	extractor RequestIDExtractor // request_id 提取器，由上层注入
}

// DialHook 实现 redis.DialHook 接口
func (h *requestIDHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

// ProcessHook 实现 redis.ProcessHook 接口
func (h *requestIDHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		// 此时 redisotel (外层 Hook) 已创建 Redis Span 并注入 Context
		// 增强当前 Redis Span 的诊断属性
		enhanceRedisSpan(ctx, cmd, h.extractor)

		// 执行底层 Redis 命令
		err := next(ctx, cmd)

		// 大厂标准：记录错误到 Span（关键！）
		if err != nil {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.RecordError(err,
					trace.WithAttributes(
						attribute.String("error.type", fmt.Sprintf("%T", err)),
						attribute.String("error.context", "redis-command-failed"),
						attribute.String("db.redis.command", cmd.Name()),
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
		setRequestIDToRedisSpan(ctx, h.extractor)

		// 执行底层 Pipeline 命令
		err := next(ctx, cmds)

		// Pipeline 错误记录：遍历每个命令，记录失败的命令到 Span
		if err != nil {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.RecordError(err,
					trace.WithAttributes(
						attribute.String("error.type", fmt.Sprintf("%T", err)),
						attribute.String("error.context", "redis-pipeline-failed"),
					),
				)
				span.SetStatus(codes.Error, fmt.Sprintf("redis pipeline failed: %v", err))
			}
		}

		return err
	}
}

// redis 命令相关常量
const (
	// evalshaKeyOffset evalsha 命令中 key 的起始偏移量
	// evalsha 命令结构: evalsha SHA1 numkeys key [key ...] arg [arg ...]
	// 索引 0=命令名, 1=SHA1, 2=numkeys, 3=第一个 key
	evalshaKeyOffset = 3

	// maxKeyDisplayLen key 属性值的最大显示长度，避免 Span 属性过长
	maxKeyDisplayLen = 100

	// maxLockNameLen 分布式锁名称在 Span 中的最大显示长度
	maxLockNameLen = 20
)

// enhanceRedisSpan 增强 Redis Span 信息（大厂标准：添加 Key、耗时等诊断属性）
func enhanceRedisSpan(ctx context.Context, cmd redis.Cmder, extractor RequestIDExtractor) {
	// 1. 提取 request_id
	setRequestIDToRedisSpan(ctx, extractor)

	// 2. 获取当前 Span
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	// 缓存 cmd.Args()，避免重复调用
	args := cmd.Args()
	commandName := cmd.Name()

	// 3. 统一 Span 名称为 redis.{command} 格式（大厂标准规范）
	// 特殊处理 evalsha：根据 Key 前缀自动识别分布式锁操作
	if commandName == "evalsha" && len(args) > evalshaKeyOffset {
		// evalsha SHA1 numkeys key [key ...] arg [arg ...]
		// 提取第一个 Key（索引 3）
		keyStr := fmt.Sprintf("%v", args[evalshaKeyOffset])

		// 根据 Key 前缀识别分布式锁操作（大厂标准）
		if strings.Contains(keyStr, "lock:") || strings.Contains(keyStr, "/dlock/") {
			// 提取锁名称（去掉前缀）
			lockName := keyStr
			if idx := strings.LastIndex(lockName, ":"); idx != -1 {
				lockName = lockName[idx+1:]
			} else if idx := strings.LastIndex(lockName, "/"); idx != -1 {
				lockName = lockName[idx+1:]
			}
			// 截取前 maxLockNameLen 个字符
			if len(lockName) > maxLockNameLen {
				lockName = lockName[:maxLockNameLen]
			}
			span.SetName("redis.lock:" + lockName)
		} else {
			// 其他 evalsha 显示 SHA1 前 8 位
			sha1 := fmt.Sprintf("%v", args[1])
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

	// 5. 提取 Key（截取前 maxKeyDisplayLen 个字符，避免过长）
	if len(args) > 1 {
		// 根据命令类型确定 key 的起始索引
		keyIndex := 1
		if commandName == "evalsha" {
			// evalsha: SHA1(1) numkeys(2) key(3) ...
			keyIndex = evalshaKeyOffset
		}
		if len(args) > keyIndex {
			keyStr := fmt.Sprintf("%v", args[keyIndex])
			if len(keyStr) > maxKeyDisplayLen {
				keyStr = keyStr[:maxKeyDisplayLen] + "..."
			}
			span.SetAttributes(attribute.String("db.redis.key", keyStr))
		}
	}
}

// setRequestIDToRedisSpan 从 Context 提取 request_id 并设置到当前 Span 属性
func setRequestIDToRedisSpan(ctx context.Context, extractor RequestIDExtractor) {
	// 如果未设置提取器，则跳过
	if extractor == nil {
		return
	}

	reqID := extractor(ctx)
	if reqID == "" {
		return
	}

	// 获取当前 Span 并设置属性
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.SetAttributes(attribute.String("request_id", reqID))
	}
}
