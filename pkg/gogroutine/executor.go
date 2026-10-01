package gogroutine

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync/atomic"
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

// taskHooks 任务执行管线的观测钩子。
// observeTask 是全局池与实例池共用的唯一执行管线；两类池的差异
// （计数归属、自定义 panic 处理）全部经钩子注入，消除双实现行为漂移
// （评审 R2-P1-3：原实现实例池不建 Span、不触达 SetMetrics 注入的采集器）。
// 约定：钩子在池创建时构造一次，任务热路径只传指针不产生分配；字段可为 nil。
type taskHooks struct {
	// taskCounter 返回本次任务归属的计数器，开始与结束必须取自同一份：
	// 全局池计数器经 atomic 重绑定，若开始/结束各 Load 一次，重绑定窗口内的
	// 在途任务会把 -1 记到新池上，令新池 Running 变负、PoolStats().Running
	// 永远等不到 ≥1（本轮实测：TestReleaseAndWaitWithTimeoutWaitForCompletion
	// 全量跑必挂、隔离跑必过，根因即在途任务的跨池减计数）。
	taskCounter  func() *instanceMetrics                                        // 返回本次任务归属的计数器；nil=不计数
	onSuccess    func()                                                         // 成功计数归属
	onPanic      func()                                                         // panic 计数归属
	panicHandler func(ctx context.Context, r interface{}, d time.Duration) bool // 自定义 panic 处理；true=已接管日志
}

// globalPoolMetrics 指向当前全局池实例的计数器。
// 由 getOrCreateGlobalPool 在 globalPoolMu 写锁内绑定；globalTaskHooks 据此维护
// running 计数，保证 PoolStats().Running 与 ReleaseAndWait 轮询口径不因管线统一而改变。
// 任务热路径经 atomic 读取，与重绑定无数据竞争。
var globalPoolMetrics atomic.Pointer[instanceMetrics]

// globalTaskHooks 全局池的管线钩子（包级单例）。
// running 计数经绑定的 globalPoolMetrics 写回全局池实例（原由 Submit 外层包装维护，
// 现统一到此处）；成功/panic 计数写入 metricsMgr（PoolStats 口径）。
var globalTaskHooks = &taskHooks{
	taskCounter: globalPoolMetrics.Load, // 方法值在包初始化时构造一次，热路径零分配
	onSuccess:   func() { metricsMgr.successCount.Add(1) },
	onPanic:     func() { metricsMgr.panicCount.Add(1) },
}

// executeTask 全局池任务执行入口（签名保持不变，executor_test/benchmark 直接调用）。
// 管线本体在 observeTask，全局池差异经 globalTaskHooks 注入。
func executeTask(ctx context.Context, name string, task func()) {
	observeTask(ctx, name, task, globalTaskHooks)
}

// observeTask 全局池与实例池共用的唯一执行管线。
// 统一处理所有任务的生命周期：创建 Span → 指标采集 → panic 恢复 → 成功计数。
//
// 设计要点：
//   - SpanKindInternal：协程池任务属于内部异步执行
//   - defer 顺序：panic 恢复在前，指标清理在后（LIFO，确保 panic 场景也能正确 dec）
//   - 命名任务便于监控系统按 name 聚合指标；实例池任务以池名作为 name
//   - 匿名任务统一标记为 "anonymous"
//   - Metrics 接口（SetMetrics 注入）对全局池与实例池一视同仁地触发
func observeTask(ctx context.Context, name string, task func(), hooks *taskHooks) {
	if name == "" {
		// 匿名任务统一标记为 "anonymous"
		name = "anonymous"
	}

	// 获取链路追踪器
	tracer := otel.Tracer("gogroutine")
	// 创建 Span
	ctx, span := tracer.Start(ctx, fmt.Sprintf("gogroutine.task.%s", name),
		trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// 设置任务初始属性
	span.SetAttributes(
		attribute.String("gogroutine.task.name", name),
		requestIDAttr(ctx),
	)

	start := time.Now()
	// 1. 指标采集：running 归属计数器在开始时取一次，结束时复用同一份（见 taskCounter 注释）
	var counter *instanceMetrics
	if hooks.taskCounter != nil {
		counter = hooks.taskCounter()
	}
	m := getMetrics()
	if m != nil {
		m.IncRunning(name)
	}
	if counter != nil {
		counter.running.Add(1)
	}

	// 2. 指标清理（LIFO：后注册先执行，确保 panic 场景也能正确 dec）
	defer func() {
		duration := time.Since(start)
		if m != nil {
			// 任务结束，指标清理
			m.DecRunning(name)
			// 记录任务耗时
			m.ObserveTaskDuration(name, duration)
		}
		if counter != nil {
			counter.running.Add(-1)
		}
		// 记录任务耗时到 Span
		span.SetAttributes(
			attribute.Float64("gogroutine.task.duration_ms", float64(duration.Milliseconds())),
		)
	}()

	// 3. panic 恢复（最内层 defer，最先执行）
	defer func() {
		if r := recover(); r != nil {
			if hooks.onPanic != nil {
				hooks.onPanic()
			}
			if m != nil {
				m.IncPanic(name)
			}
			err := fmt.Errorf("panic: %v", r)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(
				attribute.String("gogroutine.task.panic_stack", string(debug.Stack())),
			)
			duration := time.Since(start)
			// 实例池 SetPanicHandler 注入的处理器返回 true 时接管日志，避免双打
			if hooks.panicHandler == nil || !hooks.panicHandler(ctx, r, duration) {
				logger.WarnWithCtx(ctx, "gogroutine: task panic recovered",
					logger.String("name", name),
					logger.Any("panic", r),
					logger.String("duration", duration.String()),
					logger.String("stack", string(debug.Stack())),
				)
			}
		} else {
			span.SetStatus(codes.Ok, "task completed")
		}
	}()

	// 4. 执行任务本体
	task()
	if hooks.onSuccess != nil {
		hooks.onSuccess()
	}
}

// normalizeCtx 将 nil context 归一为 context.Background()。
// 导出 API 的 ctx 均为可选语义（不传 = 不需要取消控制），
// 若不归一，后续 `ctx.Done()` 会在 nil 接口上 panic（API 参数 nil 校验规范）。
func normalizeCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// shouldSkipSubmit 检查任务提交前的前置条件。
// 返回 true 表示应跳过提交（ctx 已取消）；ctx 为 nil 时不跳过（按 Background 处理）。
func shouldSkipSubmit(ctx context.Context, name string) bool {
	if ctx == nil {
		return false
	}
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
