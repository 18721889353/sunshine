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

// ---------------------------------------------------------------------------
// Mock: Lease
// ---------------------------------------------------------------------------

type mockLease struct {
	grantFn    func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error)
	keepAliveFn func(ctx context.Context, id clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error)
}

func (m *mockLease) Grant(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
	if m.grantFn != nil {
		return m.grantFn(ctx, ttl)
	}
	return &clientv3.LeaseGrantResponse{ID: clientv3.LeaseID(100)}, nil
}

func (m *mockLease) Revoke(ctx context.Context, id clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error) {
	return &clientv3.LeaseRevokeResponse{}, nil
}

func (m *mockLease) TimeToLive(ctx context.Context, id clientv3.LeaseID, opts ...clientv3.LeaseOption) (*clientv3.LeaseTimeToLiveResponse, error) {
	return &clientv3.LeaseTimeToLiveResponse{}, nil
}

func (m *mockLease) Leases(ctx context.Context) (*clientv3.LeaseLeasesResponse, error) {
	return &clientv3.LeaseLeasesResponse{}, nil
}

func (m *mockLease) KeepAlive(ctx context.Context, id clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	if m.keepAliveFn != nil {
		return m.keepAliveFn(ctx, id)
	}
	ch := make(chan *clientv3.LeaseKeepAliveResponse, 1)
	ch <- &clientv3.LeaseKeepAliveResponse{}
	return ch, nil
}

func (m *mockLease) KeepAliveOnce(ctx context.Context, id clientv3.LeaseID) (*clientv3.LeaseKeepAliveResponse, error) {
	return &clientv3.LeaseKeepAliveResponse{}, nil
}

func (m *mockLease) Close() error { return nil }

// ---------------------------------------------------------------------------
// Mock: KV
// ---------------------------------------------------------------------------

type mockKV struct {
	getFn    func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error)
	putFn    func(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error)
	deleteFn func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error)
}

func (m *mockKV) Put(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
	if m.putFn != nil {
		return m.putFn(ctx, key, val, opts...)
	}
	return &clientv3.PutResponse{}, nil
}

func (m *mockKV) Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	if m.getFn != nil {
		return m.getFn(ctx, key, opts...)
	}
	return &clientv3.GetResponse{}, nil
}

func (m *mockKV) Delete(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key, opts...)
	}
	return &clientv3.DeleteResponse{}, nil
}

func (m *mockKV) Compact(ctx context.Context, rev int64, opts ...clientv3.CompactOption) (*clientv3.CompactResponse, error) {
	return &clientv3.CompactResponse{}, nil
}

func (m *mockKV) Do(ctx context.Context, op clientv3.Op) (clientv3.OpResponse, error) {
	return clientv3.OpResponse{}, nil
}

func (m *mockKV) Txn(ctx context.Context) clientv3.Txn { return nil }

// ---------------------------------------------------------------------------
// Mock: Watcher
// ---------------------------------------------------------------------------

type mockWatcher struct {
	watchFn          func(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan
	requestProgressFn func(ctx context.Context) error
	closeFn          func() error
}

func (m *mockWatcher) Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
	if m.watchFn != nil {
		return m.watchFn(ctx, key, opts...)
	}
	return make(chan clientv3.WatchResponse)
}

func (m *mockWatcher) RequestProgress(ctx context.Context) error {
	if m.requestProgressFn != nil {
		return m.requestProgressFn(ctx)
	}
	return nil
}

func (m *mockWatcher) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newMockRegistry(ml *mockLease, mk *mockKV) *Registry {
	r := New(&clientv3.Client{Lease: ml, KV: mk},
		WithRegisterTTL(time.Second*15),
		WithContext(context.Background()),
		WithMaxRetry(3),
		WithNamespace("test"),
	)
	r.kv = mk
	return r
}

func testInstance() *registry.ServiceInstance {
	return registry.NewServiceInstance("inst-1", "test-svc", []string{"grpc://127.0.0.1:8282"},
		registry.WithVersion("v1.0.0"),
		registry.WithMetadata(map[string]string{"env": "test"}),
	)
}

// ---------------------------------------------------------------------------
// 正常流程测试
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		r := New(&clientv3.Client{})
		assert.NotNil(t, r)
		assert.Equal(t, "/microservices", r.opts.namespace)
		assert.Equal(t, 5, r.opts.maxRetry)
		assert.Equal(t, time.Second*15, r.opts.ttl)
	})

	t.Run("custom options", func(t *testing.T) {
		ctx := context.Background()
		r := New(&clientv3.Client{},
			WithContext(ctx),
			WithNamespace("/custom"),
			WithRegisterTTL(time.Second*30),
			WithMaxRetry(10),
		)
		assert.Equal(t, ctx, r.opts.ctx)
		assert.Equal(t, "/custom", r.opts.namespace)
		assert.Equal(t, time.Second*30, r.opts.ttl)
		assert.Equal(t, 10, r.opts.maxRetry)
	})
}

func TestRegister_Success(t *testing.T) {
	ml := &mockLease{}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)
	instance := testInstance()

	err := r.Register(context.Background(), instance)
	assert.NoError(t, err)
	defer r.Close()
}

func TestDeregister_Success(t *testing.T) {
	mk := &mockKV{}
	r := newMockRegistry(&mockLease{}, mk)
	instance := testInstance()

	err := r.Deregister(context.Background(), instance)
	assert.NoError(t, err)
}

func TestGetService_Success(t *testing.T) {
	si := testInstance()
	data, _ := marshal(si)
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{
				Kvs: []*mvccpb.KeyValue{
					{Key: []byte("test/test-svc/inst-1"), Value: []byte(data)},
				},
			}, nil
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	instances, err := r.GetService(context.Background(), "test-svc")
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, "inst-1", instances[0].ID)
}

func TestGetService_EmptyResult(t *testing.T) {
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{Kvs: nil}, nil
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	instances, err := r.GetService(context.Background(), "unknown-svc")
	assert.NoError(t, err)
	assert.Empty(t, instances)
}

func TestGetService_NameMismatch(t *testing.T) {
	si := registry.NewServiceInstance("inst-1", "other-svc", []string{"grpc://127.0.0.1:8282"})
	data, _ := marshal(si)
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{
				Kvs: []*mvccpb.KeyValue{
					{Key: []byte("test/other-svc/inst-1"), Value: []byte(data)},
				},
			}, nil
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	instances, err := r.GetService(context.Background(), "test-svc")
	assert.NoError(t, err)
	assert.Empty(t, instances)
}

func TestClose(t *testing.T) {
	t.Run("without register", func(t *testing.T) {
		r := newMockRegistry(&mockLease{}, &mockKV{})
		err := r.Close()
		assert.NoError(t, err)
	})

	t.Run("after register", func(t *testing.T) {
		r := newMockRegistry(&mockLease{}, &mockKV{})
		_ = r.Register(context.Background(), testInstance())
		err := r.Close()
		assert.NoError(t, err)
	})

	t.Run("multiple close", func(t *testing.T) {
		r := newMockRegistry(&mockLease{}, &mockKV{})
		_ = r.Register(context.Background(), testInstance())
		assert.NoError(t, r.Close())
		assert.NoError(t, r.Close())
	})
}

func TestRegister_Deregister_Register_Cycle(t *testing.T) {
	ml := &mockLease{}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)
	instance := testInstance()

	assert.NoError(t, r.Register(context.Background(), instance))
	assert.NoError(t, r.Deregister(context.Background(), instance))
	assert.NoError(t, r.Register(context.Background(), instance))
	assert.NoError(t, r.Close())
}

// ---------------------------------------------------------------------------
// 异常场景测试
// ---------------------------------------------------------------------------

func TestRegister_LeaseGrantError(t *testing.T) {
	ml := &mockLease{
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			return nil, errors.New("etcd connection refused")
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	err := r.Register(context.Background(), testInstance())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestRegister_KVPutError(t *testing.T) {
	ml := &mockLease{}
	mk := &mockKV{
		putFn: func(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
			return nil, errors.New("etcd put failed: timeout")
		},
	}
	r := newMockRegistry(ml, mk)

	err := r.Register(context.Background(), testInstance())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestRegister_ContextCanceled(t *testing.T) {
	ml := &mockLease{
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Register(ctx, testInstance())
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRegister_ContextDeadlineExceeded(t *testing.T) {
	ml := &mockLease{
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*10)
	defer cancel()

	err := r.Register(ctx, testInstance())
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestDeregister_DeleteError(t *testing.T) {
	mk := &mockKV{
		deleteFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
			return nil, errors.New("etcd delete failed")
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	err := r.Deregister(context.Background(), testInstance())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete failed")
}

func TestDeregister_WithoutRegister(t *testing.T) {
	mk := &mockKV{
		deleteFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
			return &clientv3.DeleteResponse{Deleted: 0}, nil
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	err := r.Deregister(context.Background(), testInstance())
	assert.NoError(t, err)
}

func TestGetService_GetError(t *testing.T) {
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return nil, errors.New("etcd get failed: connection lost")
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	_, err := r.GetService(context.Background(), "test-svc")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}

func TestGetService_UnmarshalError(t *testing.T) {
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{
				Kvs: []*mvccpb.KeyValue{
					{Key: []byte("test/test-svc/inst-1"), Value: []byte("{invalid json}")},
				},
			}, nil
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	_, err := r.GetService(context.Background(), "test-svc")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// tryReRegister 测试
// ---------------------------------------------------------------------------

func TestTryReRegister_AllRetriesExhausted(t *testing.T) {
	retryCount := 0
	ml := &mockLease{
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			retryCount++
			return nil, errors.New("persistent lease grant failure")
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	_, err := r.tryReRegister(context.Background(), "key", "value")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max retries")
	assert.Equal(t, 3, retryCount)
}

func TestTryReRegister_ContextCanceled(t *testing.T) {
	ml := &mockLease{
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			return nil, context.Canceled
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	_, err := r.tryReRegister(context.Background(), "key", "value")
	assert.Error(t, err)
}

func TestTryReRegister_ContextCanceledBetweenRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0
	ml := &mockLease{
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			callCount++
			if callCount == 2 {
				cancel()
			}
			return nil, errors.New("transient error")
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	_, err := r.tryReRegister(ctx, "key", "value")
	assert.Error(t, err)
	assert.Less(t, callCount, 4) // should stop before max retries
}

// ---------------------------------------------------------------------------
// heartBeat 测试
// ---------------------------------------------------------------------------

func TestHeartBeat_KeepAliveError(t *testing.T) {
	ml := &mockLease{
		keepAliveFn: func(ctx context.Context, id clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
			return nil, errors.New("keep alive failed")
		},
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			return nil, errors.New("grant fails")
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.heartBeat(ctx, 100, "test/key", "value")
	}()

	// heartBeat 应持续重试，不会退出
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("heartBeat should keep retrying, not exit")
	default:
	}

	cancel() // 取消后应退出
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartBeat did not exit after context cancel")
	}
}

func TestHeartBeat_KeepAliveThenReRegisterFails(t *testing.T) {
	ml := &mockLease{
		keepAliveFn: func(ctx context.Context, id clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
			return nil, errors.New("keep alive failed")
		},
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			return nil, errors.New("grant failed")
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.heartBeat(ctx, 100, "test/key", "value")
	}()

	// heartBeat 应持续重试，不会退出
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("heartBeat should keep retrying, not exit")
	default:
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartBeat did not exit after context cancel")
	}
}

func TestHeartBeat_ChannelClosed(t *testing.T) {
	ch := make(chan *clientv3.LeaseKeepAliveResponse)
	close(ch)
	callCount := 0
	ml := &mockLease{
		keepAliveFn: func(ctx context.Context, id clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
			callCount++
			if callCount >= 2 {
				return nil, errors.New("stop")
			}
			return ch, nil
		},
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			return nil, errors.New("grant fails")
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.heartBeat(ctx, 100, "test/key", "value")
	}()

	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("heartBeat should keep retrying, not exit")
	default:
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartBeat did not exit after context cancel")
	}
}

func TestHeartBeat_ContextCanceled(t *testing.T) {
	ml := &mockLease{}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.heartBeat(ctx, 100, "test/key", "value")
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartBeat did not exit after context cancel")
	}
}

func TestHeartBeat_KeepAliveThenReRegister(t *testing.T) {
	callCount := 0
	ml := &mockLease{
		keepAliveFn: func(ctx context.Context, id clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
			callCount++
			if callCount >= 3 {
				return nil, errors.New("stop")
			}
			ch := make(chan *clientv3.LeaseKeepAliveResponse, 1)
			ch <- &clientv3.LeaseKeepAliveResponse{}
			close(ch)
			return ch, nil
		},
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			return nil, errors.New("grant fails")
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.heartBeat(ctx, 100, "test/key", "value")
	}()

	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("heartBeat should keep retrying, not exit")
	default:
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartBeat did not exit after context cancel")
	}
	assert.GreaterOrEqual(t, callCount, 1)
}

func TestHeartBeat_KeyDeletedByServer_AutoRecover(t *testing.T) {
	// 模拟：第一次 KeepAlive 返回正常通道，但 Get 返回空（key 被删）
	// 应自动 Put 恢复
	getCalled := false
	putCalled := false
	ch := make(chan *clientv3.LeaseKeepAliveResponse, 1)
	ch <- &clientv3.LeaseKeepAliveResponse{}
	close(ch)

	ml := &mockLease{
		keepAliveFn: func(ctx context.Context, id clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
			return ch, nil
		},
	}
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			getCalled = true
			return &clientv3.GetResponse{Kvs: nil}, nil
		},
		putFn: func(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
			putCalled = true
			return &clientv3.PutResponse{}, nil
		},
	}
	r := newMockRegistry(ml, mk)
	r.opts.checkInterval = 1

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.heartBeat(ctx, 100, "test/key", "value")
	}()

	// 等待 verifyKey 执行完成（Put 成功），然后取消
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartBeat did not exit after context cancel")
	}
	assert.True(t, getCalled, "Get should be called to verify key")
	assert.True(t, putCalled, "Put should be called to recover deleted key")
}

func TestHeartBeat_VerifyKeyAtCheckInterval(t *testing.T) {
	// 验证 checkInterval 控制校验频率
	getCallCount := 0
	ch := make(chan *clientv3.LeaseKeepAliveResponse, 2)
	ch <- &clientv3.LeaseKeepAliveResponse{}
	ch <- &clientv3.LeaseKeepAliveResponse{}
	close(ch)

	ml := &mockLease{
		keepAliveFn: func(ctx context.Context, id clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
			return ch, nil
		},
	}
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			getCallCount++
			return &clientv3.GetResponse{
				Kvs: []*mvccpb.KeyValue{{Key: []byte(key), Value: []byte("value")}},
			}, nil
		},
	}
	r := newMockRegistry(ml, mk)
	r.opts.checkInterval = 2

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.heartBeat(ctx, 100, "test/key", "value")
	}()

	// 等心跳跑完 2 次响应，verifyKey 应只执行 1 次
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartBeat did not exit after context cancel")
	}
	assert.Equal(t, 1, getCallCount, "should verify exactly once at interval 2")
}

// ---------------------------------------------------------------------------
// registerWithKV 测试
// ---------------------------------------------------------------------------

func TestRegisterWithKV_Success(t *testing.T) {
	ml := &mockLease{}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	leaseID, err := r.registerWithKV(context.Background(), "test/key", "value")
	assert.NoError(t, err)
	assert.Equal(t, clientv3.LeaseID(100), leaseID)
}

func TestRegisterWithKV_GrantError(t *testing.T) {
	ml := &mockLease{
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			return nil, errors.New("grant error")
		},
	}
	mk := &mockKV{}
	r := newMockRegistry(ml, mk)

	_, err := r.registerWithKV(context.Background(), "test/key", "value")
	assert.Error(t, err)
}

func TestRegisterWithKV_PutError(t *testing.T) {
	ml := &mockLease{}
	mk := &mockKV{
		putFn: func(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
			return nil, errors.New("put error")
		},
	}
	r := newMockRegistry(ml, mk)

	_, err := r.registerWithKV(context.Background(), "test/key", "value")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Watch 测试
// ---------------------------------------------------------------------------

func TestWatch_RequestProgressError(t *testing.T) {
	// RequestProgress 失败
	mw := &mockWatcher{
		watchFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
			return make(chan clientv3.WatchResponse)
		},
		requestProgressFn: func(ctx context.Context) error {
			return errors.New("request progress error")
		},
	}
	client := &clientv3.Client{Watcher: mw}
	r := New(client)

	_, err := r.Watch(context.Background(), "test-svc")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "request progress")
}

// ---------------------------------------------------------------------------
// verifyKey 测试
// ---------------------------------------------------------------------------

func TestVerifyKey_KeyExists(t *testing.T) {
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{
				Kvs: []*mvccpb.KeyValue{{Key: []byte(key), Value: []byte("value")}},
			}, nil
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	newLeaseID, err := r.verifyKey(context.Background(), "test/key", 100, "value")
	assert.NoError(t, err)
	assert.Equal(t, clientv3.LeaseID(100), newLeaseID, "leaseID should remain unchanged when key exists")
}

func TestVerifyKey_KeyMissing_RewriteSuccess(t *testing.T) {
	putCalled := false
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{Kvs: nil}, nil
		},
		putFn: func(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
			putCalled = true
			assert.Equal(t, "test/key", key)
			assert.Equal(t, "value", val)
			return &clientv3.PutResponse{}, nil
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	newLeaseID, err := r.verifyKey(context.Background(), "test/key", 100, "value")
	assert.NoError(t, err)
	assert.Equal(t, clientv3.LeaseID(100), newLeaseID, "leaseID should remain unchanged on successful rewrite")
	assert.True(t, putCalled, "Put should be called when key is missing")
}

func TestVerifyKey_KeyMissing_RewriteFails_FallbackToReRegister(t *testing.T) {
	putCallCount := 0
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{Kvs: nil}, nil
		},
		putFn: func(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
			putCallCount++
			if putCallCount == 1 {
				// 第一次 Put = verifyKey 尝试用原 lease 重写 → 失败
				return nil, errors.New("lease not found")
			}
			// 第二次 Put = tryReRegister 内的 registerWithKV → 成功
			return &clientv3.PutResponse{}, nil
		},
	}
	ml := &mockLease{
		grantFn: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			return &clientv3.LeaseGrantResponse{ID: clientv3.LeaseID(200)}, nil
		},
	}
	r := newMockRegistry(ml, mk)

	newLeaseID, err := r.verifyKey(context.Background(), "test/key", 100, "value")
	assert.NoError(t, err)
	assert.Equal(t, clientv3.LeaseID(200), newLeaseID, "should get new leaseID after fallback re-register")
	assert.Equal(t, 2, putCallCount, "should call Put twice (rewrite + registerWithKV)")
}

func TestVerifyKey_GetError_ReturnsOriginalLeaseID(t *testing.T) {
	mk := &mockKV{
		getFn: func(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return nil, errors.New("etcd connection lost")
		},
	}
	r := newMockRegistry(&mockLease{}, mk)

	newLeaseID, err := r.verifyKey(context.Background(), "test/key", 100, "value")
	assert.NoError(t, err)
	assert.Equal(t, clientv3.LeaseID(100), newLeaseID, "should return original leaseID when Get fails")
}

// ---------------------------------------------------------------------------
// WithCheckInterval 测试
// ---------------------------------------------------------------------------

func TestWithCheckInterval(t *testing.T) {
	r := New(&clientv3.Client{}, WithCheckInterval(5))
	assert.Equal(t, 5, r.opts.checkInterval)

	r2 := New(&clientv3.Client{}, WithCheckInterval(0))
	assert.Equal(t, 0, r2.opts.checkInterval)

	r3 := New(&clientv3.Client{}) // 默认值
	assert.Equal(t, 3, r3.opts.checkInterval)
}
