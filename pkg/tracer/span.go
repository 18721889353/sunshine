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
	var opts []trace.SpanStartOption

	for k, v := range tags {
		var tag attribute.KeyValue
		switch val := v.(type) {
		case nil:
			continue
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
		opts = append(opts, trace.WithAttributes(tag))
	}

	return otel.Tracer(getTraceName()).Start(ctx, spanName, opts...)
}
