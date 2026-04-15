package es

import (
	"context"
	"fmt"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/tracer"
	"go.opentelemetry.io/otel/codes"
)

// withSpan 创建一个带有追踪信息的 span
func (c *Client) withSpan(ctx context.Context, operation string, opts ...interface{}) (context.Context, func(error)) {
	// 创建一个新的 span
	tags := map[string]interface{}{
		"operation": operation,
	}

	// 处理传入的参数，将它们作为属性添加到 tags 中
	for i, opt := range opts {
		tags[fmt.Sprintf("param_%d", i)] = opt
	}

	ctx, span := tracer.NewSpan(ctx, fmt.Sprintf("es.%s", operation), tags)

	// 记录操作开始的日志
	logger.DebugWithCtx(ctx, "ES operation started",
		logger.String("operation", operation),
		logger.Any("params", opts))

	// 返回上下文和结束函数
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			// 记录错误日志
			logger.ErrorWithCtx(ctx, "ES operation failed",
				logger.String("operation", operation),
				logger.Err(err),
				logger.Any("params", opts))
		} else {
			span.SetStatus(codes.Ok, "Operation successful")
			// 记录成功日志
			logger.InfoWithCtx(ctx, "ES operation completed successfully",
				logger.String("operation", operation),
				logger.Any("params", opts))
		}
		span.End()
	}
}