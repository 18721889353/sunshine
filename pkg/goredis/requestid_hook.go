package goredis

import (
	"context"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
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
		// 先设置 request_id（设置到外层 Span，如 mock-http-request）
		setRequestIDToRedisSpan(ctx)
		
		// 再执行命令（redisotel 会创建子 Span）
		return next(ctx, cmd)
	}
}

// ProcessPipelineHook 实现 redis.ProcessPipelineHook 接口
func (h *requestIDHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		// 先设置 request_id（设置到外层 Span）
		setRequestIDToRedisSpan(ctx)
		
		// 再执行命令（redisotel 会创建子 Span）
		return next(ctx, cmds)
	}
}

// setRequestIDToRedisSpan 从 Context 提取 request_id 并设置到当前 Span 属性
func setRequestIDToRedisSpan(ctx context.Context) {
	// 从 Context 中提取 request_id
	if reqID := ctx.Value("request_id"); reqID != nil {
		if reqIDStr, ok := reqID.(string); ok && reqIDStr != "" {
			// 获取当前 Span 并设置属性
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.SetAttributes(attribute.String("request_id", reqIDStr))
			}
		}
	}
}
