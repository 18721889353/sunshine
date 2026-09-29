package tracer

import (
	"context"
	"fmt"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var traceName atomic.Value

func init() {
	traceName.Store("unknown")
}

// SetTraceName 设置每个服务对应的追踪名称（ServiceName），用于区分不同服务的 Span 来源。
// 注意：SetTraceName 为进程级全局设置，多 service 场景请使用 otel.Tracer(name) 直接创建。
// 本函数是独立的原子操作，不参与 tpMu 临界区：与并发 Init/Close 的先后无保证；
// Init*/Close 内部的 traceName 更新已在 tpMu 内与 provider 替换原子一致（见 installProvider/Close）。
func SetTraceName(name string) {
	if name != "" {
		traceName.Store(name)
	}
}

// getTraceName 获取当前追踪名称。
func getTraceName() string {
	if v, ok := traceName.Load().(string); ok {
		return v
	}
	return "unknown"
}

// NewSpan 创建一个新 Span。
// 结束 Span 必须调用 span.End()。
// tags 支持 bool/string/int/int64/float64 及其切片类型，不支持的类型会转为字符串存储。
func NewSpan(ctx context.Context, spanName string, tags map[string]interface{}) (context.Context, trace.Span) {
	// 标签合并为一次 WithAttributes 传入，避免每个标签各生成一个 SpanStartOption 带来的分配开销。
	// 惰性分配：遇到第一个非 nil 标签才 make 底层数组，全 nil/空标签 map 零分配；
	// 首次分配容量取 len(tags)，常见「标签全部有效」场景与预分配同为 1 次底层数组分配，不回退。
	var attrs []attribute.KeyValue
	for k, v := range tags {
		if v == nil {
			continue
		}
		if attrs == nil {
			attrs = make([]attribute.KeyValue, 0, len(tags))
		}
		var tag attribute.KeyValue
		switch val := v.(type) {
		case bool:
			tag = attribute.Bool(k, val)
		case string:
			tag = attribute.String(k, val)
		case []string:
			tag = attribute.StringSlice(k, val)
		case int:
			tag = attribute.Int(k, val)
		case []int:
			tag = attribute.IntSlice(k, val)
		case int64:
			tag = attribute.Int64(k, val)
		case []int64:
			tag = attribute.Int64Slice(k, val)
		case float64:
			tag = attribute.Float64(k, val)
		case []float64:
			tag = attribute.Float64Slice(k, val)
		default:
			tag = attribute.String(k, fmt.Sprintf("%+v", val))
		}
		attrs = append(attrs, tag)
	}

	if len(attrs) == 0 {
		return otel.Tracer(getTraceName()).Start(ctx, spanName)
	}
	return otel.Tracer(getTraceName()).Start(ctx, spanName, trace.WithAttributes(attrs...))
}
