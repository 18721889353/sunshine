package middleware

import (
	"context"
	"fmt"
	"github.com/spf13/cast"
	"time"

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
		startTime := time.Now()

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

		// 获取客户端 IP（Gin 内置方法，支持反向代理）
		clientIP := c.ClientIP()

		// 获取 Content-Length
		requestSize := c.Request.ContentLength
		if requestSize < 0 {
			requestSize = 0
		}

		tOpts := []oteltrace.SpanStartOption{
			oteltrace.WithAttributes(
				// OpenTelemetry 标准 HTTP Server 属性
				semconv.ServiceName(serviceName),
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.URLFull(c.Request.URL.String()),
				semconv.URLPath(c.Request.URL.Path),
				semconv.URLQuery(c.Request.URL.RawQuery),
				semconv.ServerAddress(c.Request.Host),
				semconv.ServerPort(cast.ToInt(c.Request.URL.Port())),
				semconv.UserAgentOriginal(c.Request.UserAgent()),
				semconv.HTTPRoute(route),
				semconv.NetworkProtocolName(c.Request.Proto),
				semconv.NetworkProtocolVersion(fmt.Sprintf("%d.%d", c.Request.ProtoMajor, c.Request.ProtoMinor)),
				// 客户端信息
				semconv.ClientAddress(clientIP),
				semconv.NetworkPeerAddress(clientIP),
				// 请求大小
				semconv.HTTPRequestBodySize(int(requestSize)),
				// 核心：将 RequestID 作为 Span 属性，与 TraceID 关联
				attribute.String(ContextRequestIDKey, reqID),
				attribute.String("trace.request_id", reqID), // 兼容性字段
				// 其他诊断信息
				attribute.String("http.scheme", c.Request.URL.Scheme),
			),
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		}
		spanName := route
		if spanName == "" {
			spanName = fmt.Sprintf("HTTP %s route not found", c.Request.Method)
		}

		// 3. 记录请求开始事件
		ctx, span := tracer.Start(ctx, spanName, tOpts...)
		span.AddEvent("request received",
			oteltrace.WithAttributes(
				attribute.String("client_ip", clientIP),
				attribute.String("user_agent", c.Request.UserAgent()),
				attribute.Int64("request_size", requestSize),
			))
		defer span.End()

		// 4. 将 request_id 注入到 Gin Keys 和 Context，供下游组件（Redis/MySQL/RabbitMQ/Handler）使用
		c.Set(ContextRequestIDKey, reqID)
		if reqID != "" {
			ctx = context.WithValue(ctx, ContextRequestIDKey, reqID)
		}

		// 5. 将更新后的 Context 注入到 Request，确保 c.Request.Context() 能拿到 requestId
		c.Request = c.Request.WithContext(ctx)

		// serve the request to the next interceptor
		c.Next()

		// 6. 记录响应信息
		status := c.Writer.Status()
		responseSize := c.Writer.Size()
		duration := time.Since(startTime)

		// 设置完整的响应属性
		span.SetAttributes(
			semconv.HTTPResponseStatusCode(status),
			semconv.HTTPResponseBodySize(responseSize),
			attribute.Float64("http.response.duration_ms", float64(duration.Milliseconds())),
		)

		// 设置 Span 状态
		if status >= 500 {
			errorMsg := fmt.Sprintf("HTTP %d - %s %s", status, c.Request.Method, route)
			span.SetStatus(codes.Error, errorMsg)
			span.RecordError(fmt.Errorf(errorMsg),
				oteltrace.WithAttributes(
					attribute.Int("http.status_code", status),
					attribute.String("error.type", "http-server-error"),
					attribute.String("error.context", "response-status-5xx"),
				),
			)
		} else if status >= 400 {
			// 4xx 客户端错误不标记为 span error，但记录警告事件
			span.SetStatus(codes.Ok, "")
			span.AddEvent("client error",
				oteltrace.WithAttributes(
					attribute.Int("http.status_code", status),
					attribute.String("error.type", "http-client-error"),
				))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		// 记录 Gin 错误
		if len(c.Errors) > 0 {
			errors := c.Errors.String()
			span.SetAttributes(attribute.String("gin.errors", errors))
			span.RecordError(fmt.Errorf("gin errors: %s", errors),
				oteltrace.WithAttributes(
					attribute.String("error.type", "gin-handler-error"),
					attribute.String("error.context", "handler-execution-failed"),
				),
			)
		}

		// 记录请求完成事件
		span.AddEvent("request completed",
			oteltrace.WithAttributes(
				attribute.Int("http.status_code", status),
				attribute.Int64("response_size", int64(responseSize)),
				attribute.Float64("duration_ms", float64(duration.Milliseconds())),
			))
	}
}
