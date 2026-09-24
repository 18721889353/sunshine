package nacoscli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// exceededMaxRetries 测试
// ---------------------------------------------------------------------------

func TestExceededMaxRetries(t *testing.T) {
	tests := []struct {
		name    string
		max     int
		current int
		want    bool
	}{
		{"0 无限重试不超限", 0, 100, false},
		{"负数无限重试不超限", -1, 50, false},
		{"未达上限", 5, 3, false},
		{"恰好达上限", 5, 5, true},
		{"超过上限", 5, 10, true},
		{"首次即超限", 1, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exceededMaxRetries(tt.max, tt.current))
		})
	}
}

// ---------------------------------------------------------------------------
// WatchConfig 错误路径测试
// ---------------------------------------------------------------------------

// TestWatchConfigNilParams 验证 WatchConfig 对 nil params 的处理。
func TestWatchConfigNilParams(t *testing.T) {
	stop, err := WatchConfig(context.Background(), nil,
		func(_, _, _, _ string) {})
	assert.ErrorIs(t, err, ErrNilParams)
	assert.NotNil(t, stop, "错误路径也应返回非 nil stop，可安全 defer stop()")
	stop()
}

// TestWatchConfigNilHandler 验证 WatchConfig 对 nil handler 的处理。
func TestWatchConfigNilHandler(t *testing.T) {
	stop, err := WatchConfig(context.Background(),
		&Params{Group: "g", DataID: "d", Format: "yaml"}, nil)
	assert.Error(t, err)
	assert.NotNil(t, stop, "错误路径也应返回非 nil stop")
	stop()
}

// TestWatchConfigCancelledCtx 验证 WatchConfig 对已取消 ctx 的前置短路。
func TestWatchConfigCancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stop, err := WatchConfig(ctx, &Params{Group: "g", DataID: "d", Format: "yaml"},
		func(_, _, _, _ string) {})
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotNil(t, stop, "ctx 已取消也应返回非 nil stop")
	stop()
}

// TestWatchConfigInvalidFormat 验证 WatchConfig 对无效 Format 的处理。
func TestWatchConfigInvalidFormat(t *testing.T) {
	stop, err := WatchConfig(context.Background(),
		&Params{Group: "g", DataID: "d", Format: "invalid"},
		func(_, _, _, _ string) {})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不支持")
	assert.NotNil(t, stop)
	stop()
}

// TestWatchConfigWithYMLFormat 验证 WatchConfig 接受 yml 格式（归一化为 yaml）。
// 地址缺失错误在后台 goroutine 异步发生，WatchConfig 本身不报错。
func TestWatchConfigWithYMLFormat(t *testing.T) {
	stop, err := WatchConfig(context.Background(),
		&Params{Group: "g", DataID: "d", Format: "yml"},
		func(_, _, _, _ string) {},
	)
	assert.NoError(t, err, "yml 格式应通过校验")
	assert.NotNil(t, stop)
	stop()
}

// TestWatchConfigStopNotNilOnAllErrorPaths 验证所有错误路径返回非 nil stop。
func TestWatchConfigStopNotNilOnAllErrorPaths(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		params  *Params
		handler ChangeHandler
	}{
		{"ctx 已取消", cancelledCtx(), &Params{Group: "g", DataID: "d", Format: "yaml"}, func(_, _, _, _ string) {}},
		{"params 为空", context.Background(), nil, func(_, _, _, _ string) {}},
		{"handler 为空", context.Background(), &Params{Group: "g", DataID: "d", Format: "yaml"}, nil},
		{"Format 无效", context.Background(), &Params{Group: "g", DataID: "d", Format: "bad"}, func(_, _, _, _ string) {}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stop, err := WatchConfig(tt.ctx, tt.params, tt.handler)
			assert.Error(t, err)
			assert.NotNil(t, stop, "错误路径 stop 不应为 nil")
			assert.NotPanics(t, func() { stop() })
		})
	}
}

// ---------------------------------------------------------------------------
// WatchConfig 重试配置测试
// ---------------------------------------------------------------------------

// TestWatchConfigWithMaxRetries0 验证显式 WithMaxRetries(0) 可将非零值覆盖为 0（无限重试）。
func TestWatchConfigWithMaxRetries0(t *testing.T) {
	o := defaultOptions()
	o.maxRetries = 3
	WithMaxRetries(0)(o)
	assert.Equal(t, 0, o.maxRetries, "WithMaxRetries(0) 应覆盖为 0")
}

// TestWatchConfigWithMaxRetries 设置有限重试。
func TestWatchConfigWithMaxRetries(t *testing.T) {
	o := defaultOptions()
	WithMaxRetries(5)(o)
	assert.Equal(t, 5, o.maxRetries)
}

// ---------------------------------------------------------------------------
// WatchConfig stop 函数行为测试
// ---------------------------------------------------------------------------

// TestWatchConfigStopCancelsContext 验证 stop() 函数取消 ctx 并等待 goroutine 退出。
func TestWatchConfigStopCancelsContext(t *testing.T) {
	stop, err := WatchConfig(
		context.Background(),
		&Params{Group: "g", DataID: "d", Format: "yaml"},
		func(_, _, _, _ string) {},
		WithCreateDelay(10*time.Millisecond),
	)
	require.NoError(t, err)
	require.NotNil(t, stop)

	// 等待 goroutine 启动并失败一次
	time.Sleep(100 * time.Millisecond)

	// stop 应取消 ctx 并等待 goroutine 退出
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
		t.Log("stop() 正常退出")
	case <-time.After(5 * time.Second):
		t.Fatal("stop() 超时未退出")
	}
}

// TestWatchConfigErrorHandlerMsg 验证 WatchConfig 对 nil handler 返回正确的错误信息。
func TestWatchConfigErrorHandlerMsg(t *testing.T) {
	stop, err := WatchConfig(context.Background(),
		&Params{Group: "g", DataID: "d", Format: "yaml"},
		nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
	assert.NotNil(t, stop)
	assert.NotPanics(t, func() { stop() })
}

// TestWatchConfigWithNilParamsErrorMsg 验证 WatchConfig 对 nil params 返回 ErrNilParams。
func TestWatchConfigWithNilParamsErrorMsg(t *testing.T) {
	stop, err := WatchConfig(context.Background(), nil,
		func(_, _, _, _ string) {})
	assert.ErrorIs(t, err, ErrNilParams)
	assert.NotNil(t, stop)
	assert.NotPanics(t, func() { stop() })
}

// TestWatchConfigInvalidParamsErrorMsg 验证 WatchConfig 对无效参数返回校验错误。
func TestWatchConfigInvalidParamsErrorMsg(t *testing.T) {
	stop, err := WatchConfig(context.Background(),
		&Params{}, func(_, _, _, _ string) {})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
	assert.NotNil(t, stop)
	assert.NotPanics(t, func() { stop() })
}
