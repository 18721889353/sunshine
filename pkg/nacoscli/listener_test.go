package nacoscli

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NewListenClient 参数校验测试
// ---------------------------------------------------------------------------

// TestNewListenClientNilHandler 验证 NewListenClient 对 nil handler 返回错误。
func TestNewListenClientNilHandler(t *testing.T) {
	_, err := NewListenClient(&Params{Group: "g", DataID: "d", Format: "yaml"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestNewListenClientNilParams 验证 NewListenClient 对 nil params 返回错误。
func TestNewListenClientNilParams(t *testing.T) {
	_, err := NewListenClient(nil, func(_, _, _, _ string) {})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestNewListenClientInvalidParams 验证 NewListenClient 对无效 params 返回校验错误。
func TestNewListenClientInvalidParams(t *testing.T) {
	_, err := NewListenClient(
		&Params{},
		func(_, _, _, _ string) {},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "校验失败")
}

// TestNewListenClientMissingAddress 验证 NewListenClient 缺少地址时返回错误。
func TestNewListenClientMissingAddress(t *testing.T) {
	_, err := NewListenClient(
		&Params{Group: "g", DataID: "d", Format: "yaml"},
		func(_, _, _, _ string) {},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "创建 Nacos 配置监听客户端失败")
}

// ---------------------------------------------------------------------------
// ListenClient.Close 测试
// ---------------------------------------------------------------------------

// TestListenClientCloseNilConfigClient 验证 ListenClient.Close 对 nil configClient 不 panic。
func TestListenClientCloseNilConfigClient(t *testing.T) {
	listener := &ListenClient{configClient: nil}
	assert.NotPanics(t, func() { listener.Close() })
}

// TestListenClientCloseNormal 验证 ListenClient.Close 正常调用 CloseClient。
func TestListenClientCloseNormal(t *testing.T) {
	mock := &mockConfigClient{}
	listener := &ListenClient{configClient: mock}
	listener.Close()
	assert.True(t, mock.closeCalled, "应调用 CloseClient")
}

// ---------------------------------------------------------------------------
// ListenClient.Start 测试
// ---------------------------------------------------------------------------

// TestListenClientStartCtxCancel 验证 Start 在 ctx 取消时正常退出返回 nil。
func TestListenClientStartCtxCancel(t *testing.T) {
	mock := &mockConfigClient{}
	listener := &ListenClient{
		configClient: mock,
		group:        "g",
		dataID:       "d",
		handler:      func(_, _, _, _ string) {},
		param:        vo.ConfigParam{DataId: "d", Group: "g"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- listener.Start(ctx)
	}()

	// 等待 Start 注册完成并阻塞
	time.Sleep(100 * time.Millisecond)
	assert.True(t, mock.listenConfigCalled, "应调用 ListenConfig")
	assert.Equal(t, "g", mock.lastListenParam.Group)
	assert.Equal(t, "d", mock.lastListenParam.DataId)
	assert.NotNil(t, mock.lastListenParam.OnChange, "OnChange 回调应被设置")

	// 取消 ctx，Start 应返回 nil
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "ctx 取消后 Start 应返回 nil")
	case <-time.After(3 * time.Second):
		t.Fatal("Start 在 ctx 取消后未退出")
	}
}

// TestListenClientStartListenConfigError 验证 ListenConfig 失败时 Start 返回错误。
func TestListenClientStartListenConfigError(t *testing.T) {
	mock := &mockConfigClient{
		listenConfigFn: func(_ vo.ConfigParam) error {
			return fmt.Errorf("SDK 内部错误")
		},
	}
	listener := &ListenClient{
		configClient: mock,
		group:        "g",
		dataID:       "d",
		handler:      func(_, _, _, _ string) {},
		param:        vo.ConfigParam{DataId: "d", Group: "g"},
	}

	err := listener.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SDK 内部错误")
	assert.True(t, mock.listenConfigCalled, "应调用 ListenConfig")
}

// TestListenClientStartSuccessThenCtxCancel 验证 Start 在 ListenConfig 成功后阻塞，
// ctx 取消后返回 nil，且 OnChange 回调绑定正确。
func TestListenClientStartSuccessThenCtxCancel(t *testing.T) {
	var capturedOnChange vo.Listener
	mock := &mockConfigClient{
		listenConfigFn: func(p vo.ConfigParam) error {
			capturedOnChange = p.OnChange
			return nil
		},
	}
	listener := &ListenClient{
		configClient: mock,
		group:        "test-group",
		dataID:       "test-data",
		handler:      func(_, _, _, _ string) {},
		param:        vo.ConfigParam{DataId: "test-data", Group: "test-group"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- listener.Start(ctx)
	}()

	// 等待注册
	time.Sleep(50 * time.Millisecond)
	require.NotNil(t, capturedOnChange, "OnChange 应被绑定")

	// 取消
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Start 超时")
	}
}

// ---------------------------------------------------------------------------
// ListenClient.Stop 测试
// ---------------------------------------------------------------------------

// TestListenClientStopCallsCancelListenConfig 验证 Stop 调用 CancelListenConfig 并传递正确参数。
func TestListenClientStopCallsCancelListenConfig(t *testing.T) {
	mock := &mockConfigClient{}
	listener := &ListenClient{
		configClient: mock,
		group:        "myGroup",
		dataID:       "myDataID",
		param:        vo.ConfigParam{DataId: "myDataID", Group: "myGroup"},
	}

	err := listener.Stop()
	assert.NoError(t, err)
	assert.True(t, mock.cancelListenConfigCalled, "应调用 CancelListenConfig")
	assert.Equal(t, "myDataID", mock.lastCancelParam.DataId)
	assert.Equal(t, "myGroup", mock.lastCancelParam.Group)
}

// TestListenClientStopError 验证 Stop 传播 CancelListenConfig 的错误。
func TestListenClientStopError(t *testing.T) {
	mock := &mockConfigClient{
		cancelListenConfigFn: func(_ vo.ConfigParam) error {
			return fmt.Errorf("cancel failed")
		},
	}
	listener := &ListenClient{configClient: mock}
	err := listener.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancel failed")
}

// ---------------------------------------------------------------------------
// ListenClient.buildOnChange 测试
// ---------------------------------------------------------------------------

// TestBuildOnChangeCtxCancelledSkips 验证 ctx 已取消时回调跳过 handler 调用。
func TestBuildOnChangeCtxCancelledSkips(t *testing.T) {
	handlerCalled := false
	mock := &mockConfigClient{}
	listener := &ListenClient{
		configClient: mock,
		group:        "g",
		dataID:       "d",
		handler:      func(_, _, _, _ string) { handlerCalled = true },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	onChange := listener.buildOnChange(ctx)
	onChange("ns", "g", "d", "content")
	assert.False(t, handlerCalled, "ctx 已取消时不应调用 handler")
}

// TestBuildOnChangeNormalInvoke 验证 ctx 有效时回调正常触发 handler。
func TestBuildOnChangeNormalInvoke(t *testing.T) {
	var capturedNS, capturedGroup, capturedDataID, capturedData string
	handlerCalled := false

	mock := &mockConfigClient{}
	listener := &ListenClient{
		configClient: mock,
		group:        "g",
		dataID:       "d",
		handler: func(ns, grp, dID, data string) {
			handlerCalled = true
			capturedNS = ns
			capturedGroup = grp
			capturedDataID = dID
			capturedData = data
		},
	}

	onChange := listener.buildOnChange(context.Background())
	onChange("myNs", "myGroup", "myDataID", "new-content")

	assert.True(t, handlerCalled, "handler 应被调用")
	assert.Equal(t, "myNs", capturedNS)
	assert.Equal(t, "myGroup", capturedGroup)
	assert.Equal(t, "myDataID", capturedDataID)
	assert.Equal(t, "new-content", capturedData)
}

// TestBuildOnChangePanicRecover 验证 handler panic 不会传播到调用方。
func TestBuildOnChangePanicRecover(t *testing.T) {
	mock := &mockConfigClient{}
	listener := &ListenClient{
		configClient: mock,
		group:        "g",
		dataID:       "d",
		handler:      func(_, _, _, _ string) { panic("boom") },
	}

	onChange := listener.buildOnChange(context.Background())
	assert.NotPanics(t, func() {
		onChange("ns", "g", "d", "data")
	}, "buildOnChange 应 recover handler panic")
}

// TestBuildOnChangePassesCorrectArgs 验证 buildOnChange 透传 Nacos 推送的四个参数。
func TestBuildOnChangePassesCorrectArgs(t *testing.T) {
	type callbackArgs struct {
		namespace, group, dataID, data string
	}
	var args callbackArgs

	mock := &mockConfigClient{}
	listener := &ListenClient{
		configClient: mock,
		group:        "g",
		dataID:       "d",
		handler: func(ns, grp, dID, data string) {
			args = callbackArgs{ns, grp, dID, data}
		},
	}

	onChange := listener.buildOnChange(context.Background())
	onChange("prod-ns", "prod-group", "prod-data", "payload")

	assert.Equal(t, "prod-ns", args.namespace)
	assert.Equal(t, "prod-group", args.group)
	assert.Equal(t, "prod-data", args.dataID)
	assert.Equal(t, "payload", args.data)
}

// ---------------------------------------------------------------------------
// ListenClient.safeCallHandler 测试
// ---------------------------------------------------------------------------

// TestSafeCallHandlerNormal 验证 safeCallHandler 正常调用 handler。
func TestSafeCallHandlerNormal(t *testing.T) {
	called := false
	listener := &ListenClient{
		handler: func(_, _, _, data string) {
			assert.Equal(t, "test-data", data)
			called = true
		},
	}
	listener.safeCallHandler(context.Background(), "ns", "g", "d", "test-data")
	assert.True(t, called, "handler 应被调用")
}

// TestSafeCallHandlerPanicRecover 验证 safeCallHandler 捕获 handler panic 不传播。
func TestSafeCallHandlerPanicRecover(t *testing.T) {
	listener := &ListenClient{
		handler: func(_, _, _, _ string) { panic(42) },
	}
	assert.NotPanics(t, func() {
		listener.safeCallHandler(context.Background(), "ns", "g", "d", "data")
	}, "safeCallHandler 应捕获 panic")
}

// TestSafeCallHandlerAllArgs 验证 safeCallHandler 将全部参数正确传递给 handler。
func TestSafeCallHandlerAllArgs(t *testing.T) {
	var gotNS, gotGroup, gotDataID, gotData string
	listener := &ListenClient{
		handler: func(ns, grp, dID, data string) {
			gotNS, gotGroup, gotDataID, gotData = ns, grp, dID, data
		},
	}

	listener.safeCallHandler(context.Background(), "ns1", "grp1", "did1", "data1")
	assert.Equal(t, "ns1", gotNS)
	assert.Equal(t, "grp1", gotGroup)
	assert.Equal(t, "did1", gotDataID)
	assert.Equal(t, "data1", gotData)
}
