package gohttp

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// TestSpanExportedOnTransportFailure 验证 transport 失败（连接拒绝）时 Span 仍然 End 并记录 Error。
// 回归背景：resty 在 transport 错误路径直接返回、不执行 OnAfterResponse，原实现因此
// 永远不会 End Span——「最需要观测的失败请求反而零导出」。
func TestSpanExportedOnTransportFailure(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	// 127.0.0.1:1 为必然拒绝的地址（保留端口无监听），不依赖外网
	client := mustNew(t,
		WithBaseURL("http://127.0.0.1:1"),
		WithRetry(0, 0, 0),
		WithTracerProvider(tp),
	)

	_, err := client.Request(t.Context()).Get("/downstream")
	require.Error(t, err)

	ended := sr.Ended()
	require.Len(t, ended, 1, "失败请求必须导出恰好一个 Span")
	assert.Equal(t, codes.Error, ended[0].Status().Code, "失败 Span 状态应为 Error")
	require.NotEmpty(t, ended[0].Events(), "失败 Span 应记录 exception 事件")
	assert.Equal(t, "exception", ended[0].Events()[0].Name)
}

// TestSpanStatusSequenceOn502Retry 验证重试期间每个 attempt 各产生一个 Span 且状态正确：
// 两次 502 → Error，最终成功 → Ok。
func TestSpanStatusSequenceOn502Retry(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	var callCount int32
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t,
		WithBaseURL(ts.URL),
		WithRetry(3, 1*time.Millisecond, 2*time.Millisecond),
		WithTracerProvider(tp),
	)

	_, err := client.Request(t.Context()).Get("/flaky")
	require.NoError(t, err)

	assert.EqualValues(t, 3, atomic.LoadInt32(&callCount))
	assert.Equal(t,
		[]codes.Code{codes.Error, codes.Error, codes.Ok},
		spanStatuses(sr),
		"每个 attempt 一个 Span：502/502/成功")
}

// TestSpanStatusOn4xx 验证 4xx 响应将 Span 标记为 Error 并携带 http.status_code 属性
func TestSpanStatusOn4xx(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	client := mustNew(t, WithBaseURL(ts.URL), WithTracerProvider(tp))

	_, err := client.Request(t.Context()).Post("/validate")
	require.Error(t, err)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, codes.Error, ended[0].Status().Code)
	assert.Equal(t, int64(422), attrValue(ended[0].Attributes(), "http.status_code"))
}

// TestSpanStatusOn2xx 验证 2xx 响应将 Span 标记为 Ok
func TestSpanStatusOn2xx(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL), WithTracerProvider(tp))

	_, err := client.Request(t.Context()).Get("/ok")
	require.NoError(t, err)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, codes.Ok, ended[0].Status().Code)
}

// TestSpanSchemeAttribute 使用 http 明文服务时 scheme 应为 "http"。
// 回归背景：原实现硬编码 attribute.String("http.scheme", "https")，与实际协议不符。
func TestSpanSchemeAttribute(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL), WithTracerProvider(tp))

	_, err := client.Request(t.Context()).Get("/scheme")
	require.NoError(t, err)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, "http", attrString(ended[0].Attributes(), "http.scheme"))
	assert.Equal(t, "http://"+ts.URL[len("http://"):]+"/scheme",
		"http://"+attrString(ended[0].Attributes(), "http.url")[len("http://"):],
		"http.url 应记录完整请求地址")
}

// TestSpanURLEntryRedacted 验证敏感 query 参数不进入 Span 属性（APM 侧不落明文）
func TestSpanURLEntryRedacted(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL), WithTracerProvider(tp))

	_, err := client.Request(t.Context()).Get("/users?token=SUPER_SECRET&limit=10")
	require.NoError(t, err)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	urlAttr := attrString(ended[0].Attributes(), "http.url")
	assert.NotContains(t, urlAttr, "SUPER_SECRET", "敏感参数值不应出现在 Span 属性中")
	assert.Contains(t, urlAttr, "token=***")
	assert.Contains(t, urlAttr, "limit=10", "普通参数应原样保留")
}

// TestSpanNameFollowsSemconv 验证 Span 命名遵循 OTel 语义约定 `{method} {target}`
func TestSpanNameFollowsSemconv(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL), WithTracerProvider(tp))

	_, err := client.Request(t.Context()).Get("/api/v1/items?page=2")
	require.NoError(t, err)

	assert.Equal(t, []string{"GET /api/v1/items"}, spanNames(sr),
		"span 名应为「方法 + 路径」且不含 query")
}

// TestSpanRequestIDAttribute 验证 request_id 上下文写入 http.request_id 属性
// （key 命名遵循 request-id-propagation 规范的入口型前缀）
func TestSpanRequestIDAttribute(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := mustNew(t, WithBaseURL(ts.URL), WithTracerProvider(tp))

	ctx := contextWithRequestID(t.Context(), "req-abc-001")
	_, err := client.Request(ctx).Get("/rid")
	require.NoError(t, err)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, "req-abc-001", attrString(ended[0].Attributes(), "http.request_id"))
}

// TestTraceparentInjectedIntoHeader 验证启用 W3C 传播器后 traceparent 注入到请求头。
// 必须注入真实 TracerProvider：全局默认是 noop，产出的 SpanContext 无效，
// W3C 传播器对无效 TraceID 不会写入 traceparent（见 propagation/trace_context Inject）。
func TestTraceparentInjectedIntoHeader(t *testing.T) {
	withW3CPropagator(t)

	var gotTraceparent string
	ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	tp, _ := newRecordedProvider(t)
	client := mustNew(t, WithBaseURL(ts.URL), WithTracerProvider(tp))

	_, err := client.Request(t.Context()).Get("/prop")
	require.NoError(t, err)

	assert.Regexp(t, `^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`, gotTraceparent,
		"traceparent 应符合 W3C 格式")
}

// TestTransportFailureMarksEveryAttemptError 验证重试中全部 attempt 均以失败收场时
// 每个 Span 都被标记 Error（含中间 transport 失败的 attempt）
func TestTransportFailureMarksEveryAttemptError(t *testing.T) {
	tp, sr := newRecordedProvider(t)
	client := mustNew(t,
		WithBaseURL("http://127.0.0.1:1"),
		WithRetry(2, 1*time.Millisecond, 2*time.Millisecond),
		WithTracerProvider(tp),
	)

	_, err := client.Request(t.Context()).Get("/all-fail")
	require.Error(t, err)

	ended := sr.Ended()
	require.Len(t, ended, 3, "1 次初始 + 2 次重试 = 3 个 Span")
	for i, s := range ended {
		assert.Equal(t, codes.Error, s.Status().Code, "第 %d 个 Span 应为 Error", i+1)
	}
}

// attrValue 从属性列表中取数值属性
func attrValue(attrs []attribute.KeyValue, key attribute.Key) int64 {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value.AsInt64()
		}
	}
	return -1
}

// attrString 从属性列表中取字符串属性
func attrString(attrs []attribute.KeyValue, key attribute.Key) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value.AsString()
		}
	}
	return ""
}
