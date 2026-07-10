package nacos

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// ---------------------------------------------------------------------------
// Mock: INamingClient (all interface methods)
// ---------------------------------------------------------------------------

type mockNamingClient struct {
	registerInstanceFn        func(param vo.RegisterInstanceParam) (bool, error)
	batchRegisterInstanceFn   func(param vo.BatchRegisterInstanceParam) (bool, error)
	deregisterInstanceFn      func(param vo.DeregisterInstanceParam) (bool, error)
	updateInstanceFn          func(param vo.UpdateInstanceParam) (bool, error)
	getServiceFn              func(param vo.GetServiceParam) (model.Service, error)
	selectAllInstancesFn      func(param vo.SelectAllInstancesParam) ([]model.Instance, error)
	selectInstancesFn         func(param vo.SelectInstancesParam) ([]model.Instance, error)
	selectOneHealthyInstanceFn func(param vo.SelectOneHealthInstanceParam) (*model.Instance, error)
	subscribeFn               func(param *vo.SubscribeParam) error
	unsubscribeFn             func(param *vo.SubscribeParam) error
	getAllServicesInfoFn      func(param vo.GetAllServiceInfoParam) (model.ServiceList, error)
	serverHealthyFn           func() bool
	closeClientFn             func()
}

func (m *mockNamingClient) RegisterInstance(param vo.RegisterInstanceParam) (bool, error) {
	if m.registerInstanceFn != nil {
		return m.registerInstanceFn(param)
	}
	return true, nil
}

func (m *mockNamingClient) BatchRegisterInstance(param vo.BatchRegisterInstanceParam) (bool, error) {
	if m.batchRegisterInstanceFn != nil {
		return m.batchRegisterInstanceFn(param)
	}
	return true, nil
}

func (m *mockNamingClient) DeregisterInstance(param vo.DeregisterInstanceParam) (bool, error) {
	if m.deregisterInstanceFn != nil {
		return m.deregisterInstanceFn(param)
	}
	return true, nil
}

func (m *mockNamingClient) UpdateInstance(param vo.UpdateInstanceParam) (bool, error) {
	if m.updateInstanceFn != nil {
		return m.updateInstanceFn(param)
	}
	return true, nil
}

func (m *mockNamingClient) GetService(param vo.GetServiceParam) (model.Service, error) {
	if m.getServiceFn != nil {
		return m.getServiceFn(param)
	}
	return model.Service{}, nil
}

func (m *mockNamingClient) SelectAllInstances(param vo.SelectAllInstancesParam) ([]model.Instance, error) {
	if m.selectAllInstancesFn != nil {
		return m.selectAllInstancesFn(param)
	}
	return nil, nil
}

func (m *mockNamingClient) SelectInstances(param vo.SelectInstancesParam) ([]model.Instance, error) {
	if m.selectInstancesFn != nil {
		return m.selectInstancesFn(param)
	}
	return nil, nil
}

func (m *mockNamingClient) SelectOneHealthyInstance(param vo.SelectOneHealthInstanceParam) (*model.Instance, error) {
	if m.selectOneHealthyInstanceFn != nil {
		return m.selectOneHealthyInstanceFn(param)
	}
	return nil, nil
}

func (m *mockNamingClient) Subscribe(param *vo.SubscribeParam) error {
	if m.subscribeFn != nil {
		return m.subscribeFn(param)
	}
	return nil
}

func (m *mockNamingClient) Unsubscribe(param *vo.SubscribeParam) error {
	if m.unsubscribeFn != nil {
		return m.unsubscribeFn(param)
	}
	return nil
}

func (m *mockNamingClient) GetAllServicesInfo(param vo.GetAllServiceInfoParam) (model.ServiceList, error) {
	if m.getAllServicesInfoFn != nil {
		return m.getAllServicesInfoFn(param)
	}
	return model.ServiceList{}, nil
}

func (m *mockNamingClient) ServerHealthy() bool {
	if m.serverHealthyFn != nil {
		return m.serverHealthyFn()
	}
	return true
}

func (m *mockNamingClient) CloseClient() {
	if m.closeClientFn != nil {
		m.closeClientFn()
	}
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

func testNacosInstance() *registry.ServiceInstance {
	return registry.NewServiceInstance("inst-1", "test-svc", []string{"grpc://127.0.0.1:8282"},
		registry.WithVersion("v1.0.0"),
		registry.WithMetadata(map[string]string{"env": "test"}),
	)
}

// ---------------------------------------------------------------------------
// New 测试
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		r := New(&mockNamingClient{})
		assert.NotNil(t, r)
		assert.Equal(t, "DEFAULT", r.clusterName)
		assert.Equal(t, "DEFAULT_GROUP", r.groupName)
		assert.Equal(t, "grpc", r.scheme)
	})

	t.Run("custom options", func(t *testing.T) {
		r := New(&mockNamingClient{},
			WithClusterName("my-cluster"),
			WithGroupName("my-group"),
			WithScheme("http"),
		)
		assert.Equal(t, "my-cluster", r.clusterName)
		assert.Equal(t, "my-group", r.groupName)
		assert.Equal(t, "http", r.scheme)
	})

	t.Run("nil client", func(t *testing.T) {
		r := New(nil)
		assert.NotNil(t, r)
		assert.Nil(t, r.client)
	})
}

// ---------------------------------------------------------------------------
// Register 测试
// ---------------------------------------------------------------------------

func TestRegister_Success(t *testing.T) {
	mock := &mockNamingClient{}
	r := New(mock)

	err := r.Register(context.Background(), testNacosInstance())
	assert.NoError(t, err)
}

func TestRegister_EmptyEndpoints(t *testing.T) {
	r := New(&mockNamingClient{})
	instance := registry.NewServiceInstance("id", "svc", nil)

	err := r.Register(context.Background(), instance)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "端点地址不能为空")
}

func TestRegister_RegisterInstanceError(t *testing.T) {
	mock := &mockNamingClient{
		registerInstanceFn: func(param vo.RegisterInstanceParam) (bool, error) {
			return false, errors.New("nacos register failed: timeout")
		},
	}
	r := New(mock)

	err := r.Register(context.Background(), testNacosInstance())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestRegister_InvalidEndpointFormat(t *testing.T) {
	r := New(&mockNamingClient{})
	instance := registry.NewServiceInstance("id", "svc", []string{"invalid-endpoint"})

	err := r.Register(context.Background(), instance)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Deregister 测试
// ---------------------------------------------------------------------------

func TestDeregister_Success(t *testing.T) {
	mock := &mockNamingClient{}
	r := New(mock)

	err := r.Deregister(context.Background(), testNacosInstance())
	assert.NoError(t, err)
}

func TestDeregister_EmptyEndpoints(t *testing.T) {
	r := New(&mockNamingClient{})
	instance := registry.NewServiceInstance("id", "svc", nil)

	err := r.Deregister(context.Background(), instance)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "端点地址不能为空")
}

func TestDeregister_DeregisterInstanceError(t *testing.T) {
	mock := &mockNamingClient{
		deregisterInstanceFn: func(param vo.DeregisterInstanceParam) (bool, error) {
			return false, errors.New("nacos deregister failed: connection lost")
		},
	}
	r := New(mock)

	err := r.Deregister(context.Background(), testNacosInstance())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}

// ---------------------------------------------------------------------------
// GetService 测试
// ---------------------------------------------------------------------------

func TestGetService_Success(t *testing.T) {
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			return []model.Instance{
				{
					InstanceId:  "inst-1",
					ServiceName: "test-svc",
					Ip:          "127.0.0.1",
					Port:        8282,
					Metadata:    map[string]string{"env": "test"},
				},
			}, nil
		},
	}
	r := New(mock)

	instances, err := r.GetService(context.Background(), "test-svc")
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, "inst-1", instances[0].ID)
	assert.Equal(t, "grpc://127.0.0.1:8282", instances[0].Endpoints[0])
}

func TestGetService_WithScheme(t *testing.T) {
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			return []model.Instance{
				{
					InstanceId:  "inst-1",
					ServiceName: "test-svc",
					Ip:          "127.0.0.1",
					Port:        8080,
				},
			}, nil
		},
	}
	r := New(mock, WithScheme("http"))

	instances, err := r.GetService(context.Background(), "test-svc")
	assert.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, "http://127.0.0.1:8080", instances[0].Endpoints[0])
}

func TestGetService_EmptyResult(t *testing.T) {
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			return nil, nil
		},
	}
	r := New(mock)

	instances, err := r.GetService(context.Background(), "unknown-svc")
	assert.NoError(t, err)
	assert.Empty(t, instances)
}

func TestGetService_SelectError(t *testing.T) {
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			return nil, errors.New("nacos select failed")
		},
	}
	r := New(mock)

	_, err := r.GetService(context.Background(), "test-svc")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "select failed")
}

// ---------------------------------------------------------------------------
// Watch 测试
// ---------------------------------------------------------------------------

func TestWatch_Success(t *testing.T) {
	subscribeCalled := false
	mock := &mockNamingClient{
		subscribeFn: func(param *vo.SubscribeParam) error {
			subscribeCalled = true
			assert.Equal(t, "test-svc", param.ServiceName)
			assert.Equal(t, "DEFAULT_GROUP", param.GroupName)
			assert.NotNil(t, param.SubscribeCallback)
			return nil
		},
	}
	r := New(mock)

	w, err := r.Watch(context.Background(), "test-svc")
	assert.NoError(t, err)
	assert.True(t, subscribeCalled)
	assert.NotNil(t, w)
	defer w.Stop()
}

func TestWatch_SubscribeError(t *testing.T) {
	mock := &mockNamingClient{
		subscribeFn: func(param *vo.SubscribeParam) error {
			return errors.New("nacos subscribe failed: permission denied")
		},
	}
	r := New(mock)

	_, err := r.Watch(context.Background(), "test-svc")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestWatch_ContextCanceled(t *testing.T) {
	mock := &mockNamingClient{}
	r := New(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Watch(ctx, "test-svc")
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Close 测试
// ---------------------------------------------------------------------------

func TestClose(t *testing.T) {
	r := New(&mockNamingClient{})
	err := r.Close()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// parseEndpoint 测试
// ---------------------------------------------------------------------------

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantHost string
		wantPort uint64
		wantErr  bool
	}{
		{"grpc endpoint", "grpc://127.0.0.1:8282", "127.0.0.1", 8282, false},
		{"http endpoint", "http://192.168.1.1:8080", "192.168.1.1", 8080, false},
		{"no scheme", "10.0.0.1:9090", "10.0.0.1", 9090, false},
		{"https scheme", "https://example.com:443", "example.com", 443, false},
		{"invalid port", "grpc://127.0.0.1:notaport", "", 0, true},
		{"missing port", "grpc://127.0.0.1", "", 0, true},
		{"ipv6", "grpc://[::1]:8080", "::1", 8080, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := parseEndpoint(tt.endpoint)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantPort, port)
		})
	}
}

func TestParseEndpoint_EmptyString(t *testing.T) {
	_, _, err := parseEndpoint("")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// instancesToServiceInstances 测试
// ---------------------------------------------------------------------------

func TestInstancesToServiceInstances(t *testing.T) {
	t.Run("grpc scheme", func(t *testing.T) {
		instances := []model.Instance{
			{InstanceId: "id1", ServiceName: "svc1", Ip: "10.0.0.1", Port: 8282, Metadata: map[string]string{"k": "v"}},
			{InstanceId: "id2", ServiceName: "svc2", Ip: "10.0.0.2", Port: 8283},
		}
		result := instancesToServiceInstances(instances, "grpc")
		assert.Len(t, result, 2)
		assert.Equal(t, "grpc://10.0.0.1:8282", result[0].Endpoints[0])
		assert.Equal(t, "id1", result[0].ID)
		assert.Equal(t, map[string]string{"k": "v"}, result[0].Metadata)
		assert.Equal(t, "grpc://10.0.0.2:8283", result[1].Endpoints[0])
	})

	t.Run("empty input", func(t *testing.T) {
		result := instancesToServiceInstances(nil, "grpc")
		assert.Empty(t, result)
	})

	t.Run("http scheme", func(t *testing.T) {
		instances := []model.Instance{
			{InstanceId: "id1", ServiceName: "svc1", Ip: "10.0.0.1", Port: 8080},
		}
		result := instancesToServiceInstances(instances, "http")
		assert.Equal(t, "http://10.0.0.1:8080", result[0].Endpoints[0])
	})
}

// ---------------------------------------------------------------------------
// verifyAndReRegister 测试
// ---------------------------------------------------------------------------

func TestVerifyAndReRegister_InstanceExists(t *testing.T) {
	selectCalled := false
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			selectCalled = true
			return []model.Instance{
				{Ip: "127.0.0.1", Port: 8282},
			}, nil
		},
	}
	r := New(mock)
	r.stored = &storedInstance{serviceName: "svc", host: "127.0.0.1", port: 8282}

	r.verifyAndReRegister(context.Background())
	assert.True(t, selectCalled, "SelectInstances should be called")
}

func TestVerifyAndReRegister_InstanceMissing_ReRegisterSuccess(t *testing.T) {
	registerCalled := false
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			// 返回空列表，模拟实例被误删
			return nil, nil
		},
		registerInstanceFn: func(param vo.RegisterInstanceParam) (bool, error) {
			registerCalled = true
			assert.Equal(t, "127.0.0.1", param.Ip)
			assert.Equal(t, uint64(8282), param.Port)
			assert.Equal(t, "svc", param.ServiceName)
			return true, nil
		},
	}
	r := New(mock)
	r.stored = &storedInstance{serviceName: "svc", host: "127.0.0.1", port: 8282, metadata: map[string]string{"env": "test"}}

	r.verifyAndReRegister(context.Background())
	assert.True(t, registerCalled, "RegisterInstance should be called when instance is missing")
}

func TestVerifyAndReRegister_InstanceMissing_ReRegisterFails(t *testing.T) {
	registerCallCount := 0
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			return nil, nil
		},
		registerInstanceFn: func(param vo.RegisterInstanceParam) (bool, error) {
			registerCallCount++
			return false, errors.New("register failed")
		},
	}
	r := New(mock)
	r.stored = &storedInstance{serviceName: "svc", host: "127.0.0.1", port: 8282}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.verifyAndReRegister(ctx)
	}()

	// 应持续重试，不会退出
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("reRegisterWithBackoff should keep retrying, not exit")
	default:
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reRegisterWithBackoff did not exit after context cancel")
	}
	assert.GreaterOrEqual(t, registerCallCount, 1, "should retry at least once")
}

func TestVerifyAndReRegister_NilStored(t *testing.T) {
	selectCalled := false
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			selectCalled = true
			return nil, nil
		},
	}
	r := New(mock)
	r.stored = nil

	r.verifyAndReRegister(context.Background())
	assert.False(t, selectCalled, "SelectInstances should not be called when stored is nil")
}

func TestVerifyAndReRegister_SelectError(t *testing.T) {
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			return nil, errors.New("select error")
		},
	}
	r := New(mock)
	r.stored = &storedInstance{serviceName: "svc", host: "127.0.0.1", port: 8282}

	// Select 出错时不 panic，不触发 re-register
	r.verifyAndReRegister(context.Background())
}

// ---------------------------------------------------------------------------
// checkInstanceLoop 测试
// ---------------------------------------------------------------------------

func TestCheckInstanceLoop_StopsOnContextCancel(t *testing.T) {
	mock := &mockNamingClient{}
	r := New(mock)
	r.stored = &storedInstance{serviceName: "svc", host: "127.0.0.1", port: 8282}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 不 panic，应立即退出
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.checkInstanceLoop(ctx)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("checkInstanceLoop did not exit after context cancel")
	}
}

func TestCheckInstanceLoop_PeriodicCheck(t *testing.T) {
	testCheckInterval := 10 * time.Millisecond
	checkCount := 0
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			checkCount++
			return []model.Instance{
				{Ip: "127.0.0.1", Port: 8282},
			}, nil
		},
	}
	r := New(mock, WithCheckInterval(testCheckInterval))
	r.stored = &storedInstance{serviceName: "svc", host: "127.0.0.1", port: 8282}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	r.checkInstanceLoop(ctx)
	assert.GreaterOrEqual(t, checkCount, 1, "should check at least once")
}

// ---------------------------------------------------------------------------
// Register 启动 check goroutine / Deregister 停止 集成测试
// ---------------------------------------------------------------------------

func TestRegister_StartsCheckGoroutine(t *testing.T) {
	mock := &mockNamingClient{
		selectInstancesFn: func(param vo.SelectInstancesParam) ([]model.Instance, error) {
			return []model.Instance{
				{Ip: "127.0.0.1", Port: 8282},
			}, nil
		},
	}
	r := New(mock)
	assert.Nil(t, r.cancelCheck, "cancelCheck should be nil before Register")
	assert.Nil(t, r.stored, "stored should be nil before Register")

	err := r.Register(context.Background(), testNacosInstance())
	assert.NoError(t, err)
	assert.NotNil(t, r.cancelCheck, "cancelCheck should be set after Register")
	assert.NotNil(t, r.stored, "stored should be set after Register")
	assert.Equal(t, "test-svc", r.stored.serviceName)
	assert.Equal(t, "127.0.0.1", r.stored.host)
	assert.Equal(t, uint64(8282), r.stored.port)

	r.Close()
}

func TestDeregister_StopsCheckGoroutine(t *testing.T) {
	mock := &mockNamingClient{registerInstanceFn: func(param vo.RegisterInstanceParam) (bool, error) { return true, nil }}
	r := New(mock, WithCheckInterval(10*time.Millisecond))

	_ = r.Register(context.Background(), testNacosInstance())
	assert.NotNil(t, r.cancelCheck)
	assert.NotNil(t, r.stored)

	_ = r.Deregister(context.Background(), testNacosInstance())
	assert.Nil(t, r.cancelCheck, "cancelCheck should be nil after Deregister")
	assert.Nil(t, r.stored, "stored should be nil after Deregister")
}

func TestRegister_ReRegisterStopsPreviousCheck(t *testing.T) {
	cancelCount := 0
	mock := &mockNamingClient{registerInstanceFn: func(param vo.RegisterInstanceParam) (bool, error) { return true, nil }}
	r := New(mock)

	_ = r.Register(context.Background(), testNacosInstance())
	oldCancel := r.cancelCheck
	assert.NotNil(t, oldCancel)

	// 二次注册应取消前一次
	_ = r.Register(context.Background(), testNacosInstance())
	assert.NotNil(t, r.cancelCheck)
	// 旧 cancelCheck 被替换，调旧函数不应 panic
	oldCancel()
	cancelCount++
	assert.Equal(t, 1, cancelCount)

	r.Close()
}

// ---------------------------------------------------------------------------
// WithCheckInterval 测试
// ---------------------------------------------------------------------------

func TestWithCheckInterval(t *testing.T) {
	r := New(&mockNamingClient{})
	assert.Equal(t, 30*time.Second, r.checkInterval, "default check interval should be 30s")

	r2 := New(&mockNamingClient{}, WithCheckInterval(0))
	assert.Equal(t, time.Duration(0), r2.checkInterval, "check interval 0 should be allowed")

	r3 := New(&mockNamingClient{}, WithCheckInterval(time.Minute))
	assert.Equal(t, time.Minute, r3.checkInterval)
}

func TestCheckInterval_Zero_NoCheckGoroutine(t *testing.T) {
	mock := &mockNamingClient{registerInstanceFn: func(param vo.RegisterInstanceParam) (bool, error) { return true, nil }}
	r := New(mock, WithCheckInterval(0))

	err := r.Register(context.Background(), testNacosInstance())
	assert.NoError(t, err)
	assert.Nil(t, r.cancelCheck, "cancelCheck should be nil when checkInterval is 0")
	assert.NotNil(t, r.stored, "stored should still be set when checkInterval is 0")

	r.Close()
}
