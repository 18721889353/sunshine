package dlock

// dlock 埋点统一走本文件，遵循 pkg/tracer/status.go 全库口径：
// tracer scope 单 token "dlock"；span 名 dlock.<operation>（lock/unlock/trylock，低基数），
// 锁键名放属性 dlock.key（高基数不进 span 名）；SpanKind 为 Client；
// 状态成功 MarkOK、真故障 MarkErr；TryLock 未抢锁属正常竞争结果，按 OK 处理不染红。
// 带 span 的 ctx 会传入底层 redsync/etcd 操作，若底层 redis/etcd 客户端已埋点则自动嵌套子 span。

import (
	"context"
	"errors"

	"github.com/go-redsync/redsync/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/tracer"
)

// dlockTracer 是 dlock 包唯一 tracer scope（规范：包名单一小写 token）。
var dlockTracer = otel.Tracer("dlock")

// startLockSpan 起锁操作 Client span，operation 为 lock/unlock/trylock，key 作属性。
func startLockSpan(ctx context.Context, operation, key string) (context.Context, trace.Span) {
	return dlockTracer.Start(ctx, "dlock."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("dlock.system", "distributed_lock"),
			attribute.String("dlock.operation", operation),
			attribute.String("dlock.key", key),
		))
}

// markLock 依据 err 收尾 Lock/Unlock span（err==nil→OK，否则→Error）。
func markLock(span trace.Span, err error) {
	if err != nil {
		tracer.MarkErr(span, err)
		return
	}
	tracer.MarkOK(span)
}

// markTryLock 收尾 TryLock span：记录是否抢到锁；
// 真故障才染红，未抢锁/正常竞争视作正常结果标 OK。
func markTryLock(span trace.Span, acquired bool, err error) {
	span.SetAttributes(attribute.Bool("dlock.acquired", acquired))
	if err != nil && !isLockContention(err) {
		tracer.MarkErr(span, err)
		return
	}
	tracer.MarkOK(span)
}

// isLockContention 判定 redsync TryLock 返回的错误是否为“锁已被占用”这类正常竞争结果，
// 而非基础设施故障；正常竞争不应在链路中标记为 Error。
// redsync v4.13.0：争用达 quorum 时 actOnPoolsAsync 返回 *ErrTaken，
// 部分节点占用则以 multierror 累积 *ErrNodeTaken（multierror 支持 errors.As 透传）。
func isLockContention(err error) bool {
	var taken *redsync.ErrTaken
	var nodeTaken *redsync.ErrNodeTaken
	return errors.As(err, &taken) || errors.As(err, &nodeTaken)
}
