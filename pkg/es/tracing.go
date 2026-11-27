package es

import (
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// esTracer ES 客户端 tracer（与全仓 otel.Tracer 命名约定一致）。
var esTracer = otel.Tracer("es")

// tracingRoundTripper 在 transport 层统一埋点——一处覆盖所有 ES 请求
// （search/bulk/document/security/healthcheck），业务侧零改动。
type tracingRoundTripper struct {
	next http.RoundTripper
}

func (t *tracingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// span 名用 method + path（如 "POST /_search"），与 OTel ES 语义约定对齐
	spanName := req.Method + " " + req.URL.Path
	ctx, span := esTracer.Start(req.Context(), spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.HTTPRequestMethodKey.String(req.Method),
			semconv.URLPath(req.URL.Path),
			attribute.String("es.addresses", req.URL.Host),
		),
	)
	defer span.End()

	resp, err := t.next.RoundTrip(req.WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return resp, err
	}

	statusCode := resp.StatusCode
	span.SetAttributes(semconv.HTTPResponseStatusCode(statusCode))
	// 4xx/5xx 视为错误（与全仓 result 口径一致：非 2xx 即 error）
	if statusCode >= 400 {
		span.SetStatus(codes.Error, "ES responded with "+strconv.Itoa(statusCode))
	}
	return resp, nil
}
