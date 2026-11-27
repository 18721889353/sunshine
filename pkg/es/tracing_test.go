package es

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeRoundTripper 返回可控响应，验证 tracingRoundTripper 不破坏原始调用。
type fakeRoundTripper struct {
	status int
	err    error
	gotCtx context.Context
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.gotCtx = req.Context()
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

// TestTracingRoundTripper_Success 成功路径：透传响应，且请求 ctx 带上了 span。
func TestTracingRoundTripper_Success(t *testing.T) {
	next := &fakeRoundTripper{status: 200}
	rt := &tracingRoundTripper{next: next}
	req, _ := http.NewRequest("POST", "http://es:9200/_search", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode 应透传 200, got %d", resp.StatusCode)
	}
	if next.gotCtx == nil || next.gotCtx == req.Context() {
		t.Error("下游请求 ctx 应携带新 span")
	}
}

// TestTracingRoundTripper_Error 下游报错：错误原样透传（span 内记录，不影响调用方）。
func TestTracingRoundTripper_Error(t *testing.T) {
	wantErr := fmt.Errorf("connection refused")
	rt := &tracingRoundTripper{next: &fakeRoundTripper{err: wantErr}}
	req, _ := http.NewRequest("GET", "http://es:9200/_cluster/health", nil)

	_, err := rt.RoundTrip(req)
	if err != wantErr {
		t.Errorf("错误应原样透传, got %v", err)
	}
}

// TestTracingRoundTripper_HTTPError 4xx/5xx 响应原样透传（span 标错误不影响调用方）。
func TestTracingRoundTripper_HTTPError(t *testing.T) {
	rt := &tracingRoundTripper{next: &fakeRoundTripper{status: 503}}
	req, _ := http.NewRequest("GET", "http://es:9200/_cluster/health", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil || resp.StatusCode != 503 {
		t.Errorf("503 应原样透传: resp=%v err=%v", resp, err)
	}
}
