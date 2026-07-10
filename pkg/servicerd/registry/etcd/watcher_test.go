package etcd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

func newWatch(first bool) *watcher {
	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*2)

	return &watcher{
		key:         "/test/test-svc",
		ctx:         ctx,
		cancel:      cancelFunc,
		watchChan:   make(clientv3.WatchChan),
		watcher:     &mockWatcher{},
		kv:          &mockKV{},
		first:       first,
		serviceName: "test-svc",
	}
}

func newWatchWithData(instances []*registry.ServiceInstance, first bool) *watcher {
	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*2)

	// 序列化实例
	var kvs []*mvccpb.KeyValue
	for _, inst := range instances {
		data, _ := marshal(inst)
		kvs = append(kvs, &mvccpb.KeyValue{
			Key:   []byte("/test/" + inst.Name + "/" + inst.ID),
			Value: []byte(data),
		})
	}

	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{Kvs: kvs}, nil
		},
	}

	return &watcher{
		key:         "/test/test-svc",
		ctx:         ctx,
		cancel:      cancelFunc,
		watchChan:   make(clientv3.WatchChan),
		watcher:     &mockWatcher{},
		kv:          mk,
		first:       first,
		serviceName: "test-svc",
	}
}

// ---------------------------------------------------------------------------
// Watcher Next 测试
// ---------------------------------------------------------------------------

func TestWatcher_Next_First(t *testing.T) {
	inst := registry.NewServiceInstance("inst-1", "test-svc", []string{"grpc://127.0.0.1:8282"})
	w := newWatchWithData([]*registry.ServiceInstance{inst}, true)

	instances, err := w.Next()
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, "inst-1", instances[0].ID)
	assert.False(t, w.first, "first should be false after first Next")
}

func TestWatcher_Next_First_NoInstances(t *testing.T) {
	w := newWatchWithData(nil, true)

	instances, err := w.Next()
	assert.NoError(t, err)
	assert.Empty(t, instances)
	assert.False(t, w.first)
}

func TestWatcher_Next_ContextCanceled(t *testing.T) {
	w := newWatch(false)
	w.cancel() // 立刻取消

	_, err := w.Next()
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWatcher_Next_WatchChannelClosed(t *testing.T) {
	ch := make(chan clientv3.WatchResponse)
	close(ch)
	inst := registry.NewServiceInstance("inst-1", "test-svc", []string{"grpc://127.0.0.1:8282"})
	w := newWatchWithData([]*registry.ServiceInstance{inst}, false)
	w.watchChan = ch

	instances, err := w.Next()
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
}

func TestWatcher_Next_ContextBeforeWatchChan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := newWatch(false)
	w.ctx = ctx
	w.cancel = cancel

	cancel() // 先取消，确保 ctx.Done() 优先于 watchChan

	_, err := w.Next()
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Watcher Stop 测试
// ---------------------------------------------------------------------------

func TestWatcher_Stop(t *testing.T) {
	w := newWatch(false)
	err := w.Stop()
	assert.NoError(t, err)
	assert.Error(t, w.ctx.Err()) // context 已被取消
}

func TestWatcher_Stop_Multiple(t *testing.T) {
	w := newWatch(false)
	assert.NoError(t, w.Stop())
	assert.NoError(t, w.Stop()) // 多次 Stop 不应 panic
}

func TestWatcher_Stop_CloseError(t *testing.T) {
	w := newWatch(false)
	w.watcher = &mockWatcher{
		closeFn: func() error {
			return errors.New("close error")
		},
	}
	err := w.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "close error")
}

// ---------------------------------------------------------------------------
// getInstance 测试
// ---------------------------------------------------------------------------

func TestGetInstance_Success(t *testing.T) {
	inst := registry.NewServiceInstance("inst-1", "test-svc", []string{"grpc://127.0.0.1:8282"})
	w := newWatchWithData([]*registry.ServiceInstance{inst}, false)

	instances, err := w.getInstance()
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
}

func TestGetInstance_MultipleInstances(t *testing.T) {
	inst1 := registry.NewServiceInstance("inst-1", "test-svc", []string{"grpc://127.0.0.1:8282"})
	inst2 := registry.NewServiceInstance("inst-2", "test-svc", []string{"grpc://127.0.0.1:8283"})
	w := newWatchWithData([]*registry.ServiceInstance{inst1, inst2}, false)

	instances, err := w.getInstance()
	assert.NoError(t, err)
	assert.Len(t, instances, 2)
}

func TestGetInstance_NameFilter(t *testing.T) {
	// 匹配的
	inst1 := registry.NewServiceInstance("inst-1", "test-svc", []string{"grpc://127.0.0.1:8282"})
	// 不匹配的
	inst2 := registry.NewServiceInstance("inst-2", "other-svc", []string{"grpc://127.0.0.1:8283"})
	w := newWatchWithData([]*registry.ServiceInstance{inst1, inst2}, false)

	instances, err := w.getInstance()
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, "inst-1", instances[0].ID)
}

func TestGetInstance_KVGetError(t *testing.T) {
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return nil, errors.New("kv get error")
		},
	}
	w := newWatch(false)
	w.kv = mk

	_, err := w.getInstance()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kv get error")
}

func TestGetInstance_UnmarshalError(t *testing.T) {
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{
				Kvs: []*mvccpb.KeyValue{
					{Key: []byte("test/key"), Value: []byte("{invalid}")},
				},
			}, nil
		},
	}
	w := newWatch(false)
	w.kv = mk

	_, err := w.getInstance()
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// marshal / unmarshal 测试
// ---------------------------------------------------------------------------

func TestServiceMarshal_Unmarshal(t *testing.T) {
	instance := registry.NewServiceInstance("foo", "bar", []string{"grpc://127.0.0.1:8282"})
	v, err := marshal(instance)
	assert.NoError(t, err)

	si, err := unmarshal([]byte(v))
	assert.NoError(t, err)
	assert.Equal(t, instance, si)
}

func TestServiceMarshal_NilInstance(t *testing.T) {
	v, err := marshal(nil)
	assert.NoError(t, err)
	assert.Equal(t, "null", v)
}

func TestServiceUnmarshal_EmptyData(t *testing.T) {
	_, err := unmarshal([]byte{})
	assert.Error(t, err)
}

func TestServiceUnmarshal_InvalidJSON(t *testing.T) {
	_, err := unmarshal([]byte("{not valid json}"))
	assert.Error(t, err)
}
