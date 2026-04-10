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
		// 执行原命令（包括 redisotel 的 tracing hook）
		// redisotel 会在内部创建 Span，我们需要在它结束前设置 request_id
		err := next(ctx, cmd)

		// 在命令执行后，从 Context 提取 request_id 并设置到当前 Span
		// 注意：此时 redisotel 的 Span 还未结束（defer span.End() 尚未执行）
		setRequestIDToRedisSpan(ctx)

		return err
	}
}

// ProcessPipelineHook 实现 redis.ProcessPipelineHook 接口
func (h *requestIDHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		// 执行原命令（包括 redisotel 的 tracing hook）
		err := next(ctx, cmds)

		// 在命令执行后，从 Context 提取 request_id 并设置到当前 Span
		setRequestIDToRedisSpan(ctx)

		return err
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
