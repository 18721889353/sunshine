package nacos

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// ---------------------------------------------------------------------------
// watcher 测试
// ---------------------------------------------------------------------------

func TestWatcher_Next_ReturnsInstances(t *testing.T) {
	ch := make(chan []*registry.ServiceInstance, 1)
	ch <- []*registry.ServiceInstance{
		registry.NewServiceInstance("inst-1", "svc", []string{"grpc://127.0.0.1:8282"}),
	}
	w := &watcher{
		serviceName: "svc",
		groupName:   "DEFAULT_GROUP",
		scheme:      "grpc",
		ctx:         context.Background(),
		cancel:      func() {},
		ch:          ch,
	}

	instances, err := w.Next()
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, "inst-1", instances[0].ID)
}

func TestWatcher_Next_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := &watcher{
		ctx:    ctx,
		cancel: cancel,
		ch:     make(chan []*registry.ServiceInstance, 1),
	}

	_, err := w.Next()
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWatcher_Next_BlocksUntilData(t *testing.T) {
	ch := make(chan []*registry.ServiceInstance, 1)
	w := &watcher{
		ctx:    context.Background(),
		cancel: func() {},
		ch:     ch,
	}

	resultCh := make(chan error, 1)
	go func() {
		_, err := w.Next()
		resultCh <- err
	}()

	// 确认 Next 阻塞中
	select {
	case <-resultCh:
		t.Fatal("Next should block until data arrives")
	case <-time.After(time.Millisecond * 50):
	}

	// 发送数据
	ch <- []*registry.ServiceInstance{
		registry.NewServiceInstance("inst-1", "svc", []string{"grpc://127.0.0.1:8282"}),
	}

	select {
	case err := <-resultCh:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Next did not return after data sent")
	}
}

// ---------------------------------------------------------------------------
// Stop 测试
// ---------------------------------------------------------------------------

func TestWatcher_Stop(t *testing.T) {
	unsubscribed := false
	mock := &mockNamingClient{
		unsubscribeFn: func(param *vo.SubscribeParam) error {
			unsubscribed = true
			assert.Equal(t, "svc", param.ServiceName)
			assert.Equal(t, "test-group", param.GroupName)
			return nil
		},
	}

	w := &watcher{
		client:      mock,
		serviceName: "svc",
		groupName:   "test-group",
		ctx:         context.Background(),
		cancel:      func() {},
		ch:          make(chan []*registry.ServiceInstance, 1),
	}

	err := w.Stop()
	assert.NoError(t, err)
	assert.True(t, unsubscribed)
}

func TestWatcher_Stop_UnsubscribeError(t *testing.T) {
	mock := &mockNamingClient{
		unsubscribeFn: func(param *vo.SubscribeParam) error {
			return errors.New("unsubscribe failed")
		},
	}

	w := &watcher{
		client:      mock,
		serviceName: "svc",
		groupName:   "test-group",
		ctx:         context.Background(),
		cancel:      func() {},
		ch:          make(chan []*registry.ServiceInstance, 1),
	}

	err := w.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsubscribe failed")
}

func TestWatcher_Stop_CancelsContext(t *testing.T) {
	mock := &mockNamingClient{}
	w := &watcher{
		client:      mock,
		serviceName: "svc",
		groupName:   "test-group",
		ch:          make(chan []*registry.ServiceInstance, 1),
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())

	_ = w.Stop()
	assert.Error(t, w.ctx.Err()) // context 已被取消
}

func TestWatcher_Next_ReturnsAfterStop(t *testing.T) {
	mock := &mockNamingClient{}
	w := &watcher{
		client:      mock,
		serviceName: "svc",
		groupName:   "test-group",
		ch:          make(chan []*registry.ServiceInstance, 1),
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	_ = w.Stop()

	_, err := w.Next()
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// callback 测试
// ---------------------------------------------------------------------------

func TestCallback_SendsToChannel(t *testing.T) {
	ch := make(chan []*registry.ServiceInstance, 1)
	w := &watcher{
		ctx:    context.Background(),
		cancel: func() {},
		ch:     ch,
		scheme: "grpc",
	}

	services := []model.Instance{
		{InstanceId: "id1", ServiceName: "svc", Ip: "10.0.0.1", Port: 8282},
	}
	w.callback(services, nil)

	select {
	case instances := <-ch:
		assert.Len(t, instances, 1)
		assert.Equal(t, "id1", instances[0].ID)
	default:
		t.Fatal("expected data in channel after callback")
	}
}

func TestCallback_ErrorIgnored(t *testing.T) {
	ch := make(chan []*registry.ServiceInstance, 1)
	w := &watcher{
		ctx:    context.Background(),
		cancel: func() {},
		ch:     ch,
	}

	// 回调带 error 时应忽略
	w.callback(nil, errors.New("some error"))

	select {
	case <-ch:
		t.Fatal("should not send data when callback has error")
	default:
	}
}

func TestCallback_ContextDone_Drops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan []*registry.ServiceInstance, 1)
	w := &watcher{
		ctx:    ctx,
		cancel: cancel,
		ch:     ch,
		scheme: "grpc",
	}

	services := []model.Instance{
		{InstanceId: "id1", ServiceName: "svc", Ip: "10.0.0.1", Port: 8282},
	}

	// context 已取消，callback 应 drop 消息
	w.callback(services, nil)

	select {
	case <-ch:
		t.Fatal("should not send when context is done")
	default:
	}
}

func TestCallback_FullChannel_Drops(t *testing.T) {
	ch := make(chan []*registry.ServiceInstance, 1) // buffer=1
	// 填满 channel
	ch <- []*registry.ServiceInstance{
		registry.NewServiceInstance("id1", "svc", []string{"grpc://127.0.0.1:8282"}),
	}

	w := &watcher{
		ctx:    context.Background(),
		cancel: func() {},
		ch:     ch,
		scheme: "grpc",
	}

	services := []model.Instance{
		{InstanceId: "id2", ServiceName: "svc", Ip: "10.0.0.2", Port: 8283},
	}

	// channel 满，callback 应 drop 新消息
	w.callback(services, nil)

	assert.Len(t, ch, 1) // 仍然只有原始的一条
}

// ---------------------------------------------------------------------------
// 集成场景：Watch → Next → Stop
// ---------------------------------------------------------------------------

func TestWatcher_Integration(t *testing.T) {
	var mu sync.Mutex
	var callbackFn func(services []model.Instance, err error)

	mock := &mockNamingClient{
		subscribeFn: func(param *vo.SubscribeParam) error {
			mu.Lock()
			callbackFn = param.SubscribeCallback
			mu.Unlock()
			return nil
		},
		unsubscribeFn: func(param *vo.SubscribeParam) error {
			return nil
		},
	}

	r := New(mock, WithGroupName("test-group"))
	w, err := r.Watch(context.Background(), "test-svc")
	assert.NoError(t, err)
	assert.NotNil(t, w)

	// 模拟 Nacos 服务变更回调
	mu.Lock()
	cb := callbackFn
	mu.Unlock()
	assert.NotNil(t, cb)

	cb([]model.Instance{
		{InstanceId: "inst-1", ServiceName: "test-svc", Ip: "10.0.0.1", Port: 8282},
	}, nil)

	instances, err := w.Next()
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, "inst-1", instances[0].ID)

	// Stop 后不应再收到数据
	assert.NoError(t, w.Stop())
	_, err = w.Next()
	assert.Error(t, err)
}
