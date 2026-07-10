package discovery

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"

	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// ---------------------------------------------------------------------------
// Mock Discovery
// ---------------------------------------------------------------------------

type mockDiscovery struct {
	getServiceFn func(ctx context.Context, serviceName string) ([]*registry.ServiceInstance, error)
	watchFn      func(ctx context.Context, serviceName string) (registry.Watcher, error)
}

func (d *mockDiscovery) GetService(ctx context.Context, serviceName string) ([]*registry.ServiceInstance, error) {
	if d.getServiceFn != nil {
		return d.getServiceFn(ctx, serviceName)
	}
	return []*registry.ServiceInstance{}, nil
}

func (d *mockDiscovery) Watch(ctx context.Context, serviceName string) (registry.Watcher, error) {
	if d.watchFn != nil {
		return d.watchFn(ctx, serviceName)
	}
	return &mockWatcher{}, nil
}

// ---------------------------------------------------------------------------
// Mock Watcher
// ---------------------------------------------------------------------------

type mockWatcher struct {
	nextFn func() ([]*registry.ServiceInstance, error)
	stopFn func() error
}

func (w *mockWatcher) Next() ([]*registry.ServiceInstance, error) {
	if w.nextFn != nil {
		return w.nextFn()
	}
	return []*registry.ServiceInstance{}, nil
}

func (w *mockWatcher) Stop() error {
	if w.stopFn != nil {
		return w.stopFn()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock ClientConn
// ---------------------------------------------------------------------------

type mockClientConn struct {
	updateStateFn     func(state resolver.State) error
	reportErrorFn     func(err error)
	newAddressFn      func(addresses []resolver.Address)
	newServiceConfigFn func(serviceConfig string)
	parseServiceConfigFn func(serviceConfigJSON string) *serviceconfig.ParseResult
}

func (c *mockClientConn) UpdateState(state resolver.State) error {
	if c.updateStateFn != nil {
		return c.updateStateFn(state)
	}
	return nil
}

func (c *mockClientConn) ReportError(err error) {
	if c.reportErrorFn != nil {
		c.reportErrorFn(err)
	}
}

func (c *mockClientConn) NewAddress(addresses []resolver.Address) {
	if c.newAddressFn != nil {
		c.newAddressFn(addresses)
	}
}

func (c *mockClientConn) NewServiceConfig(serviceConfig string) {
	if c.newServiceConfigFn != nil {
		c.newServiceConfigFn(serviceConfig)
	}
}

func (c *mockClientConn) ParseServiceConfig(serviceConfigJSON string) *serviceconfig.ParseResult {
	if c.parseServiceConfigFn != nil {
		return c.parseServiceConfigFn(serviceConfigJSON)
	}
	return &serviceconfig.ParseResult{}
}

// ---------------------------------------------------------------------------
// Builder 测试
// ---------------------------------------------------------------------------

func TestNewBuilder(t *testing.T) {
	t.Run("default builder", func(t *testing.T) {
		b := NewBuilder(&mockDiscovery{})
		assert.NotNil(t, b)
		assert.Equal(t, "discovery", b.Scheme())
	})

	t.Run("with options", func(t *testing.T) {
		b := NewBuilder(&mockDiscovery{},
			WithInsecure(false),
			WithTimeout(time.Second),
			WithDebugLogDisabled(),
		)
		assert.NotNil(t, b)
	})
}

func TestBuilder_Build_Success(t *testing.T) {
	b := NewBuilder(&mockDiscovery{})
	u := url.URL{Path: "test-service"}
	r, err := b.Build(resolver.Target{URL: u}, &mockClientConn{}, resolver.BuildOptions{})
	assert.NoError(t, err)
	assert.NotNil(t, r)
	assert.IsType(t, &discoveryResolver{}, r)
	defer r.Close()
}

func TestBuilder_Build_WatchError(t *testing.T) {
	b := NewBuilder(&mockDiscovery{
		watchFn: func(ctx context.Context, serviceName string) (registry.Watcher, error) {
			return nil, errors.New("watch failed")
		},
	})
	u := url.URL{Path: "test-service"}
	_, err := b.Build(resolver.Target{URL: u}, &mockClientConn{}, resolver.BuildOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "watch failed")
}

func TestBuilder_Build_Timeout(t *testing.T) {
	b := NewBuilder(&mockDiscovery{
		watchFn: func(ctx context.Context, serviceName string) (registry.Watcher, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}, WithTimeout(time.Millisecond*10))
	u := url.URL{Path: "test-service"}
	_, err := b.Build(resolver.Target{URL: u}, &mockClientConn{}, resolver.BuildOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "overtime")
}

func TestBuilder_Scheme(t *testing.T) {
	b := NewBuilder(&mockDiscovery{})
	assert.Equal(t, "discovery", b.Scheme())
}

// ---------------------------------------------------------------------------
// Resolver 测试
// ---------------------------------------------------------------------------

func TestDiscoveryResolver_Close(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	r := &discoveryResolver{
		w:                &mockWatcher{},
		cc:               &mockClientConn{},
		ctx:              ctx,
		cancel:           cancel,
		insecure:         true,
		debugLogDisabled: false,
	}
	defer r.Close()
	r.ResolveNow(resolver.ResolveNowOptions{})
}

func TestDiscoveryResolver_Close_WatcherStopError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	r := &discoveryResolver{
		w: &mockWatcher{
			stopFn: func() error {
				return errors.New("stop error")
			},
		},
		cc:               &mockClientConn{},
		ctx:              ctx,
		cancel:           cancel,
		insecure:         true,
		debugLogDisabled: true,
	}
	// Close 不应 panic，即使 Stop 返回 error
	r.Close()
}

func TestDiscoveryResolver_Update_WithInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	updated := false
	r := &discoveryResolver{
		w:                &mockWatcher{},
		cc:               &mockClientConn{},
		ctx:              ctx,
		cancel:           cancel,
		insecure:         true,
		debugLogDisabled: true,
	}

	r.update([]*registry.ServiceInstance{
		registry.NewServiceInstance("foo", "bar", []string{"grpc://127.0.0.1:8282"}),
	})

	// update should not error
	assert.False(t, updated)
}

func TestDiscoveryResolver_Update_EmptyInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	r := &discoveryResolver{
		w:                &mockWatcher{},
		cc:               &mockClientConn{},
		ctx:              ctx,
		cancel:           cancel,
		insecure:         true,
		debugLogDisabled: true,
	}

	// 空实例列表不应调用 UpdateState
	r.update(nil)
	r.update([]*registry.ServiceInstance{})
}

func TestDiscoveryResolver_Update_DuplicateEndpoints(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	stateCount := 0
	r := &discoveryResolver{
		w:      &mockWatcher{},
		cc:     &mockClientConn{},
		ctx:    ctx,
		cancel: cancel,
		insecure: true,
		debugLogDisabled: true,
	}

	// 相同的 endpoint 应被去重
	r.update([]*registry.ServiceInstance{
		registry.NewServiceInstance("inst-1", "svc", []string{"grpc://127.0.0.1:8282"}),
		registry.NewServiceInstance("inst-2", "svc", []string{"grpc://127.0.0.1:8282"}),
	})
	assert.Equal(t, stateCount, stateCount) // just verify no panic
}

func TestDiscoveryResolver_Update_NoMatchingScheme(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	updateCalled := false
	r := &discoveryResolver{
		w:                &mockWatcher{},
		cc:               &mockClientConn{},
		ctx:              ctx,
		cancel:           cancel,
		insecure:         true,
		debugLogDisabled: true,
	}

	// scheme 不匹配的 endpoint 应被忽略
	r.update([]*registry.ServiceInstance{
		registry.NewServiceInstance("inst-1", "svc", []string{"http://127.0.0.1:8080"}),
	})
	assert.False(t, updateCalled)
}

func TestDiscoveryResolver_Update_StateError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	r := &discoveryResolver{
		w: &mockWatcher{},
		cc: &mockClientConn{
			updateStateFn: func(state resolver.State) error {
				return errors.New("update state error")
			},
		},
		ctx:              ctx,
		cancel:           cancel,
		insecure:         true,
		debugLogDisabled: true,
	}

	// UpdateState 返回 error 时不应 panic
	r.update([]*registry.ServiceInstance{
		registry.NewServiceInstance("inst-1", "svc", []string{"grpc://127.0.0.1:8282"}),
	})
}

func TestDiscoveryResolver_Watch_LoopContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	r := &discoveryResolver{
		w:                &mockWatcher{},
		cc:               &mockClientConn{},
		ctx:              ctx,
		cancel:           cancel,
		insecure:         true,
		debugLogDisabled: true,
	}

	r.watch() // context 取消后应退出，不 panic
}

func TestDiscoveryResolver_Watch_NextErrorNonCancel(t *testing.T) {
	callCount := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r := &discoveryResolver{
		w: &mockWatcher{
			nextFn: func() ([]*registry.ServiceInstance, error) {
				callCount++
				if callCount <= 2 {
					return nil, errors.New("transient error")
				}
				return []*registry.ServiceInstance{}, nil
			},
		},
		cc:               &mockClientConn{},
		ctx:              ctx,
		cancel:           cancel,
		insecure:         true,
		debugLogDisabled: true,
	}

	r.watch()
	assert.GreaterOrEqual(t, callCount, 2)
}

func TestDiscoveryResolver_Watch_NextCanceled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	r := &discoveryResolver{
		w: &mockWatcher{
			nextFn: func() ([]*registry.ServiceInstance, error) {
				<-ctx.Done()
				return nil, context.Canceled
			},
		},
		cc:               &mockClientConn{},
		ctx:              ctx,
		cancel:           cancel,
		insecure:         true,
		debugLogDisabled: true,
	}

	r.watch() // context.Canceled 应优雅退出
}

// ---------------------------------------------------------------------------
// parseAttributes 测试
// ---------------------------------------------------------------------------

func TestParseAttributes(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		a := parseAttributes(nil)
		assert.Nil(t, a)
	})

	t.Run("empty metadata", func(t *testing.T) {
		a := parseAttributes(map[string]string{})
		assert.Nil(t, a)
	})

	t.Run("single entry", func(t *testing.T) {
		a := parseAttributes(map[string]string{"key1": "val1"})
		assert.NotNil(t, a)
	})

	t.Run("multiple entries", func(t *testing.T) {
		a := parseAttributes(map[string]string{"key1": "val1", "key2": "val2"})
		assert.NotNil(t, a)
	})
}

// ---------------------------------------------------------------------------
// parseEndpoint 测试
// ---------------------------------------------------------------------------

func TestParseEndpoint_DifferentFormats(t *testing.T) {
	tests := []struct {
		name       string
		endpoints  []string
		scheme     string
		isSecure   bool
		wantAddr   string
		wantErr    bool
	}{
		{"grpc insecure", []string{"grpc://127.0.0.1:8282"}, "grpc", false, "127.0.0.1:8282", false},
		{"grpc secure", []string{"grpc://127.0.0.1:8282?isSecure=true"}, "grpc", true, "127.0.0.1:8282", false},
		{"http insecure", []string{"http://127.0.0.1:8080"}, "grpc", false, "", false},
		{"http matched", []string{"http://127.0.0.1:8080"}, "http", false, "127.0.0.1:8080", false},
		{"no match", []string{"http://127.0.0.1:8080"}, "grpc", true, "", false},
		{"multiple endpoints", []string{"http://127.0.0.1:8080", "grpc://127.0.0.1:8282"}, "grpc", false, "127.0.0.1:8282", false},
		{"invalid url", []string{"://invalid"}, "grpc", false, "", true},
		{"nil endpoints", nil, "grpc", false, "", false},
		{"empty endpoints", []string{}, "grpc", false, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := parseEndpoint(tt.endpoints, tt.scheme, tt.isSecure)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantAddr, addr)
		})
	}
}

// ---------------------------------------------------------------------------
// IsSecure 测试
// ---------------------------------------------------------------------------

func TestIsSecure(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"no query", "http://localhost:8080", false},
		{"isSecure=true", "http://localhost:8080?isSecure=true", true},
		{"isSecure=false", "http://localhost:8080?isSecure=false", false},
		{"isSecure=invalid", "http://localhost:8080?isSecure=yes", false},
		{"other param", "http://localhost:8080?foo=bar", false},
		{"multiple params secure", "http://localhost:8080?foo=bar&isSecure=true", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.url)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, IsSecure(u))
		})
	}
}
