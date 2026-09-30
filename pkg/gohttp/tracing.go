package gohttp

import (
	"context"
	"fmt"
	"net/url"
	"reflect"

	"github.com/go-resty/resty/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// trackedSpan 记录单个 attempt 的 Span 与其状态设置情况，保证状态只被设置一次
type trackedSpan struct {
	span      trace.Span
	statusSet bool
}

// trackedSpanList 按 attempt 顺序保存一次请求产生的全部 Span。
//
// 为什么需要 tracker：resty 对每次重试都执行一遍 OnBeforeRequest（每 attempt 一个 Span），
// 而 OnAfterResponse 只在 transport 成功时执行——transport 失败（DNS 失败/连接拒绝/超时）
// 的 attempt 拿不到任何收尾机会。若只存单个 Span 引用，失败请求的 Span 将永远不 End，
// 即「最需要被观测的失败请求反而零导出」。tracker 由 do() 统一兜底收尾，见 finishSpans。
type trackedSpanList struct {
	spans []trackedSpan
}

// add 追加一个新 attempt 的 Span
func (t *trackedSpanList) add(span trace.Span) {
	t.spans = append(t.spans, trackedSpan{span: span})
}

// last 返回最近一个 attempt 的 Span 记录，列表为空时返回 nil
func (t *trackedSpanList) last() *trackedSpan {
	if len(t.spans) == 0 {
		return nil
	}
	return &t.spans[len(t.spans)-1]
}

// markLastError 将最近一个尚未定状态的 Span 标记为 Error 并记录错误事件
func (t *trackedSpanList) markLastError(recErr error, statusMsg string) {
	if cur := t.last(); cur != nil && !cur.statusSet {
		cur.span.SetStatus(codes.Error, statusMsg)
		cur.span.RecordError(recErr)
		cur.statusSet = true
	}
}

// markLastOK 将最近一个尚未定状态的 Span 标记为 Ok
func (t *trackedSpanList) markLastOK() {
	if cur := t.last(); cur != nil && !cur.statusSet {
		cur.span.SetStatus(codes.Ok, "")
		cur.statusSet = true
	}
}

// finishAllError 为所有尚未定状态的 Span 补记 Error 并全部 End。
// 幂等：已定状态的 attempt（走完 OnAfterResponse 或重试钩子）不会被重复标记。
func (t *trackedSpanList) finishAllError(finalErr error) {
	for i := range t.spans {
		if !t.spans[i].statusSet {
			t.spans[i].span.SetStatus(codes.Error, finalErr.Error())
			t.spans[i].span.RecordError(finalErr)
			t.spans[i].statusSet = true
		}
	}
	t.endAll()
}

// endAll End 全部 Span（只由 do() 收尾路径调用一次）
func (t *trackedSpanList) endAll() {
	for i := range t.spans {
		t.spans[i].span.End()
	}
}

// isUsableTracerProvider 判定 TracerProvider 是否可用于注入：
// nil 与 typed nil（reflect 判定指针/接口/函数/Map/Slice 底层为 nil）都视为未注入，
// 回退全局 TracerProvider——防止 typed nil 调用 Tracer() 时产生不可预期行为。
func isUsableTracerProvider(tp trace.TracerProvider) bool {
	if tp == nil {
		return false
	}
	v := reflect.ValueOf(tp)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return !v.IsNil()
	}
	return true
}

// urlTarget 从完整 URL 提取 spanName 用的 target（仅路径，不含 query，避免敏感参数
// 进入按名称聚合的维度）；无路径时归一为 "/"。
func urlTarget(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return "/"
	}
	return u.Path
}

// urlScheme 从完整 URL 提取协议（解析失败返回空串）。修复原实现硬编码 "https" 的问题。
func urlScheme(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Scheme
}

// resolveRequestURL 把请求 URL 归一为完整地址（供 Span 名与 http.url/http.scheme 属性使用）。
//
// 为什么需要自己拼：resty 的**用户自定义** OnBeforeRequest 循环先于其内置的
// parseRequestURL 执行（见 resty client.go execute：udBeforeRequest 循环在
// beforeRequest 循环之前），拦截器看到的 req.URL 仍是调用方传入的相对路径
// （如 "/scheme"），scheme 属性会变为空串。这里按 resty 同样的规则
// （url.Parse 判 IsAbs，否则拼 BaseURL）提前归一，并附上已设置的查询参数
// （查询参数由 resty 内置中间件在其后合并，拦截时还不在 URL 上）。
func resolveRequestURL(c *Client, rawURL string, query url.Values) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if !u.IsAbs() {
		base := c.cli.BaseURL
		if base == "" {
			base = c.cli.HostURL
		}
		path := rawURL
		if len(path) > 0 && path[0] != '/' {
			path = "/" + path
		}
		if merged, err := url.Parse(base + path); err == nil {
			u = merged
		}
	}
	if len(query) > 0 && u.RawQuery == "" {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
// key 命名遵循 request-id-propagation 规范（入口型协议前缀）：http.request_id。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("http.request_id", reqID)
		}
	}
	return attribute.String("http.request_id", "")
}

// onBeforeRequest 请求前置拦截：创建 Span、注入传播头、执行安全守卫（熔断/大小上限）。
// resty 对每次重试都调用一次，因此每 attempt 各产生一个 Span 并加入 tracker。
func (c *Client) onBeforeRequest(_ *resty.Client, req *resty.Request) error {
	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// 复用本次请求已有的 tracker（重试场景），否则新建
	tracker, ok := ctx.Value(httpSpanContextKey).(*trackedSpanList)
	if !ok || tracker == nil {
		tracker = &trackedSpanList{}
	}

	// 从已设置的请求头提取传播上下文（支持调用方预设 traceparent）；
	// 未设置时 Extract 原样返回 ctx，父 Span 仍由本地 ctx 决定。
	carrier := propagation.HeaderCarrier(req.Header)
	extractedCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

	// Span 命名遵循 OTel 语义约定：`{method} {target}`（target 仅路径）
	// 注意此处必须先归一为完整 URL（见 resolveRequestURL 注释）
	method := req.Method
	urlStr := resolveRequestURL(c, req.URL, req.QueryParam)
	spanName := method + " " + urlTarget(urlStr)

	spanCtx, span := c.tracer.Start(extractedCtx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.method", method),
			attribute.String("http.url", redactURL(urlStr)), // 敏感 query 参数不进入 APM
			attribute.String("http.scheme", urlScheme(urlStr)),
			requestIDAttr(ctx),
		),
	)
	tracker.add(span)

	// 将新的 span context 注入到请求头中（W3C traceparent）
	otel.GetTextMapPropagator().Inject(spanCtx, propagation.HeaderCarrier(req.Header))

	// 携带 tracker 与 spanCtx 进入后续阶段
	req.SetContext(context.WithValue(spanCtx, httpSpanContextKey, tracker))

	// 尝试从 context 中自动捞出分布式链路追踪 TraceID（兼容旧逻辑）
	if traceID, ok := ctx.Value(traceIDContextKey).(string); ok && traceID != "" {
		req.SetHeader("X-Trace-ID", traceID)
	}

	req.SetHeader("User-Agent", "Golang-GoHttp-Enterprise/v2.0")

	// 安全守卫：熔断器与请求体上限（拒绝时返回错误中止请求，
	// 该 attempt 的 Span 由 do() 收尾并记录拒绝原因）
	if err := c.breaker.allow(); err != nil {
		return err
	}
	return c.checkSizeLimit(req)
}

// onAfterResponse 响应拦截：为 transport 成功的 attempt 设置状态码属性与 Span 状态。
// 注意这里**不调用 span.End()**——统一由 do() 的 finishSpans 兜底收尾，
// 保证 transport 失败的 attempt 也能被导出。
func (c *Client) onAfterResponse(_ *resty.Client, resp *resty.Response) error {
	if resp == nil || resp.Request == nil {
		return nil
	}
	ctx := resp.Request.Context()
	if ctx == nil {
		return nil
	}
	tracker, ok := ctx.Value(httpSpanContextKey).(*trackedSpanList)
	if !ok || tracker == nil {
		return nil
	}
	cur := tracker.last()
	if cur == nil || cur.statusSet {
		return nil
	}

	statusCode := resp.StatusCode()
	cur.span.SetAttributes(
		attribute.Int("http.status_code", statusCode),
		attribute.Int64("http.response_content_length", resp.Size()),
	)

	// 4xx/5xx 记为错误（OTel HTTP 语义约定：client span 的 4xx/5xx 均为 Error）
	if statusCode >= 400 {
		tracker.markLastError(
			fmt.Errorf("http request failed with status %d", statusCode),
			fmt.Sprintf("HTTP %d", statusCode),
		)
	} else {
		tracker.markLastOK()
	}

	cur.span.AddEvent("response received",
		trace.WithAttributes(
			attribute.Int("http.status_code", statusCode),
			attribute.Int64("http.response_content_length", resp.Size()),
		),
	)
	return nil
}

// onRetryAttempt resty 重试钩子：为 transport 失败的中间 attempt 补记 Error 状态。
// 依据 resty v2.16.5 retry.go：满足重试条件即执行钩子，且最后一次重试同样执行，
// 因此每个失败 attempt 都能在 do() 收尾前拿到自己的错误；成功响应（如 502 后重试）
// 的状态已由 onAfterResponse 设置，此处经 statusSet 标记跳过，不会重复记录。
func (c *Client) onRetryAttempt(resp *resty.Response, err error) {
	if err == nil || resp == nil || resp.Request == nil {
		return
	}
	ctx := resp.Request.Context()
	if ctx == nil {
		return
	}
	tracker, ok := ctx.Value(httpSpanContextKey).(*trackedSpanList)
	if !ok || tracker == nil {
		return
	}
	tracker.markLastError(err, err.Error())
}
