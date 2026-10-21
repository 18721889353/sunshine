package tracer

// 本文件是 sunshine 全库 OTel 埋点口径的唯一约束源（single source of truth）。
// 所有库内 IO 埋点应统一走下列辅助函数，杜绝“失败设 Error、成功漏设 OK 留 Unset”的漂移，
// 监控（如 ARMS）里成功链路因此一致显示为 OK。
//
// 统一规范（后续新增埋点必须遵循）：
//  1. tracer scope：包名单一小写 token（如 gomq / goemail / kafka / dlock / goredis），
//     禁止 goemail.smtp 式多级点分；子组件细节体现在 span 名里。
//  2. span 名：<system>.<operation>.<target> 小写点分
//     （如 rabbitmq.publish.<exchange>、kafka.receive.<topic>、dlock.lock.<key>、redis.<cmd>）。
//  3. SpanKind 精确：DB / HTTP / 缓存 / 分布式锁 → SpanKindClient；
//     MQ 发送 → SpanKindProducer；MQ 接收 → SpanKindConsumer。
//  4. 属性键：优先使用 OTel semconv 标准键（messaging.* / db.* / http.*）；
//     自定义键用 <system>.<字段> 命名空间，避免裸名冲突。
//  5. 状态口径：成功 → MarkOK（codes.Ok 且不带描述，遵循 OTel 规范 OK 状态描述应为空）；
//     失败 → MarkErr（RecordError + codes.Error + 错误描述）。

import (
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// MarkOK 将 span 标记为成功。
// 按 OTel 规范，codes.Ok 状态不应携带描述，故统一传空串。
// span 为 nil 时空操作，便于在“可能未创建 span”的路径安全调用。
func MarkOK(span trace.Span) {
	if span == nil {
		return
	}
	span.SetStatus(codes.Ok, "")
}

// MarkErr 记录 err 并将其作为 span 的错误描述，状态置为 codes.Error。
// span 为 nil 或 err 为 nil 时空操作（err==nil 属成功，不应走此通道，避免误染红）。
// opts 透传给 RecordError，可附加错误事件属性（如 trace.WithAttributes）。
func MarkErr(span trace.Span, err error, opts ...trace.EventOption) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err, opts...)
	span.SetStatus(codes.Error, err.Error())
}

// EndSpan 依据 err 的成功/失败统一收尾 span：
// err==nil 标 OK，否则标 Error，最后 End。
// 适用于“defer tracer.EndSpan(span, &err)”不便、需在确定 err 后显式收尾的场景；
// 若用 defer 收尾，需配合命名返回值指针以读取最终 err（见下方用法说明）。
//
// 用法（显式收尾）：
//
//	err := doWork()
//	tracer.EndSpan(span, err) // 内部已 End，勿再调用 span.End()
func EndSpan(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err == nil {
		MarkOK(span)
	} else {
		MarkErr(span, err)
	}
	span.End()
}
