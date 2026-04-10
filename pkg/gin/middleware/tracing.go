package middleware

import (
	"fmt"
	"github.com/gin-gonic/gin"
	otelcontrib "go.opentelemetry.io/contrib"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	tracerKey  = "otel-tracer"
	tracerName = "otelgin"
)

type traceConfig struct {
	TracerProvider oteltrace.TracerProvider
	Propagators    propagation.TextMapPropagator
}

// TraceOption specifies instrumentation configuration options.
type TraceOption func(*traceConfig)

// WithPropagators specifies propagators to use for extracting
// information from the HTTP requests. If none are specified, global
// ones will be used.
func WithPropagators(propagators propagation.TextMapPropagator) TraceOption {
	return func(cfg *traceConfig) {
		cfg.Propagators = propagators
	}
}

// WithTracerProvider specifies a tracer provider to use for creating a tracer.
// If none is specified, the global provider is used.
func WithTracerProvider(provider oteltrace.TracerProvider) TraceOption {
	return func(cfg *traceConfig) {
		cfg.TracerProvider = provider
	}
}

// Tracing returns interceptor that will trace incoming requests.
// The service parameter should describe the name of the (virtual)
// server handling the request.
func Tracing(serviceName string, opts ...TraceOption) gin.HandlerFunc {
	cfg := traceConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.TracerProvider == nil {
		cfg.TracerProvider = otel.GetTracerProvider()
	}
	tracer := cfg.TracerProvider.Tracer(
		tracerName,
		oteltrace.WithInstrumentationVersion(otelcontrib.Version()),
	)
	if cfg.Propagators == nil {
		cfg.Propagators = otel.GetTextMapPropagator()
	}

	return func(c *gin.Context) {
		// 1. 获取 RequestID（优先使用 RequestID 作为链路标识）
		reqID := c.Request.Header.Get(HeaderXRequestIDKey)
		if reqID == "" {
			if v, isExist := c.Get(ContextRequestIDKey); isExist {
				if requestID, ok := v.(string); ok {
					reqID = requestID
				}
			}
		}

		c.Set(tracerKey, tracer)
		savedCtx := c.Request.Context()
		defer func() {
			c.Request = c.Request.WithContext(savedCtx)
		}()

		// 2. 从 HTTP Header 提取 Trace Context（支持跨服务链路传递）
		ctx := cfg.Propagators.Extract(savedCtx, propagation.HeaderCarrier(c.Request.Header))
		route := c.FullPath()

		tOpts := []oteltrace.SpanStartOption{
			oteltrace.WithAttributes(
				// HTTP server attributes (v1.40.0)
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.URLFull(c.Request.URL.String()),
				semconv.URLPath(c.Request.URL.Path),
				semconv.URLQuery(c.Request.URL.RawQuery),
				semconv.ServerAddress(c.Request.Host),
				semconv.UserAgentOriginal(c.Request.UserAgent()),
				semconv.HTTPRoute(route),
				// 核心：将 RequestID 作为 Span 属性，与 TraceID 关联
				attribute.String("request_id", reqID),
				attribute.String("trace.request_id", reqID), // 兼容性字段
			),
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		}
		spanName := route
		if spanName == "" {
			spanName = fmt.Sprintf("HTTP %s route not found", c.Request.Method)
		}

		// 3. 创建 Span
		ctx, span := tracer.Start(ctx, spanName, tOpts...)
		defer span.End()

		// 4. 将 Span 的 TraceID 注入到 Context，便于下游使用
		// 注意：TraceID 是 OpenTelemetry 自动生成的 UUID，但我们有 reqID 作为业务标识
		c.Request = c.Request.WithContext(ctx)

		// serve the request to the next interceptor
		c.Next()

		status := c.Writer.Status()
		// Set HTTP response status code attribute
		span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		// Set span status based on HTTP status code
		if status >= 500 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", status))
		} else if status >= 400 {
			// 4xx errors are not set as span errors by default
			span.SetStatus(codes.Ok, "")
		} else {
			span.SetStatus(codes.Ok, "")
		}
		if len(c.Errors) > 0 {
			span.SetAttributes(attribute.String("gin.errors", c.Errors.String()))
		}
	}
}
