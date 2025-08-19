package es

import (
	"context"
	"fmt"

	"github.com/18721889353/sunshine/pkg/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// instrumentationName 是用于追踪的 instrumentation 名称
	instrumentationName = "github.com/18721889353/sunshine/pkg/es"
)

// withSpan 创建一个带有追踪信息的 span
func (c *Client) withSpan(ctx context.Context, operation string, opts ...interface{}) (context.Context, func(error)) {
	// 创建一个新的 span
	ctx, span := tracer.NewSpan(ctx, fmt.Sprintf("es.%s", operation), nil)
	
	// 返回上下文和结束函数
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "Operation successful")
		}
		span.End()
	}
}

// addSpanAttributes 为 span 添加属性
func addSpanAttributes(span trace.Span, attrs map[string]interface{}) {
	// 为 span 添加属性
	for k, v := range attrs {
		switch val := v.(type) {
		case string:
			span.SetAttributes(attribute.String(k, val))
		case int:
			span.SetAttributes(attribute.Int(k, val))
		case int64:
			span.SetAttributes(attribute.Int64(k, val))
		case float64:
			span.SetAttributes(attribute.Float64(k, val))
		case bool:
			span.SetAttributes(attribute.Bool(k, val))
		default:
			span.SetAttributes(attribute.String(k, fmt.Sprintf("%v", val)))
		}
	}
}