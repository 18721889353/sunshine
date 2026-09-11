package gogroutine

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ============================================================================
// 链路追踪辅助
// ============================================================================

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("gogroutine.request_id", reqID)
		}
	}
	return attribute.String("gogroutine.request_id", "")
}

// ============================================================================
// 任务执行器 - 统一 panic 恢复 + 指标监控 + 链路追踪
// ============================================================================

// executeTask 带指标监控、panic 恢复和链路追踪的任务执行器。
// 统一处理所有任务的生命周期：创建 Span → 指标采集 → panic 恢复 → 成功计数。
//
// 设计要点：
//   - SpanKindInternal：协程池任务属于内部异步执行
//   - defer 顺序：panic 恢复在前，指标清理在后（LIFO，确保 panic 场景也能正确 dec）
//   - 命名任务便于监控系统按 name 聚合指标
//   - 匿名任务统一标记为 "anonymous"
func executeTask(ctx context.Context, name string, task func()) {
	if name == "" {
		name = "anonymous"
	}

	// 1. 创建链路追踪 Span
	tracer := otel.Tracer("gogroutine")
	ctx, span := tracer.Start(ctx, fmt.Sprintf("gogroutine.task.%s", name),
		trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// 设置任务初始属性
	span.SetAttributes(
		attribute.String("gogroutine.task.name", name),
		requestIDAttr(ctx),
	)

	start := time.Now()
	m := getMetrics()
	if m != nil {
		m.IncRunning(name)
	}

	// 2. 指标清理（LIFO：后注册先执行，确保 panic 场景也能正确 dec）
	defer func() {
		duration := time.Since(start)
		if m != nil {
			m.DecRunning(name)
			m.ObserveTaskDuration(name, duration)
		}
		// 记录任务耗时到 Span
		span.SetAttributes(
			attribute.Float64("gogroutine.task.duration_ms", float64(duration.Milliseconds())),
		)
	}()

	// 3. panic 恢复（最内层 defer，最先执行）
	defer func() {
		if r := recover(); r != nil {
			panicCount.Add(1)
			if m != nil {
				m.IncPanic(name)
			}
			err := fmt.Errorf("panic: %v", r)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(
				attribute.String("gogroutine.task.panic_stack", string(debug.Stack())),
			)
			logger.ErrorWithCtx(ctx, "goroutine panic recovered",
				logger.String("name", name),
				logger.Any("panic", r),
				logger.String("duration", time.Since(start).String()),
				logger.String("stack", string(debug.Stack())),
			)
		} else {
			span.SetStatus(codes.Ok, "task completed")
		}
	}()

	// 4. 执行任务本体
	task()
	successCount.Add(1)
}

// shouldSkipSubmit 检查任务提交前的前置条件。
// 返回 true 表示应跳过提交（ctx 已取消或 task 为 nil）。
func shouldSkipSubmit(ctx context.Context, name string, _ func()) bool {
	// context 已取消，任务无需入队
	select {
	case <-ctx.Done():
		logger.WarnWithCtx(ctx, "gogroutine: skip submit, context cancelled",
			logger.String("name", name),
			logger.Any("err", ctx.Err()),
		)
		return true
	default:
	}
	return false
}
