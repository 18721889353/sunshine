package etcdcli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestNewClient(t *testing.T) {
	endpoints := []string{"192.168.3.37:2379"}
	cli, err := NewClient(endpoints,
		WithDialTimeout(time.Second*2),
		WithAuth("", ""),
		WithAutoSyncInterval(0),
	)
	t.Log(err, cli)

	cli, err = NewClient(nil, WithConfig(&clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: time.Second * 2,
		Username:    "",
		Password:    "",
	}))
	t.Log(err, cli)

	// test error
	_, err = NewClient(endpoints,
		WithDialTimeout(time.Second),
		WithSecure("foo", "notfound.crt"))
	assert.Error(t, err)
	endpoints = nil
	_, err = NewClient(endpoints)
	assert.Error(t, err)
}

func TestWithAuthConfig(t *testing.T) {
	o := defaultOptions()
	o.apply(WithAuthConfig(AuthConfig{
		TokenTTL:        5 * time.Minute,
		RefreshInterval: 3 * time.Minute,
	}))

	assert.NotNil(t, o.authConfig)
	assert.Equal(t, 5*time.Minute, o.authConfig.TokenTTL)
	assert.Equal(t, 3*time.Minute, o.authConfig.RefreshInterval)
}

func TestWithAuthConfigDefaultRefreshInterval(t *testing.T) {
	o := defaultOptions()
	o.apply(WithAuthConfig(AuthConfig{
		TokenTTL: 5 * time.Minute,
	}))

	assert.NotNil(t, o.authConfig)
	assert.Equal(t, 5*time.Minute, o.authConfig.TokenTTL)
	assert.Equal(t, time.Duration(0), o.authConfig.RefreshInterval) // 未设置，将使用默认值
}

func TestStopTokenRefresh_NilClient(t *testing.T) {
	// nil 客户端不应 panic
	StopTokenRefresh(nil)
}

func TestResolveRefreshInterval(t *testing.T) {
	// 用户指定 RefreshInterval 时优先使用
	assert.Equal(t, 3*time.Minute, resolveRefreshInterval(AuthConfig{
		TokenTTL:        5 * time.Minute,
		RefreshInterval: 3 * time.Minute,
	}))

	// 未指定 RefreshInterval 时取 TokenTTL 的 60%
	assert.Equal(t, 3*time.Minute, resolveRefreshInterval(AuthConfig{
		TokenTTL: 5 * time.Minute,
	}))

	// TokenTTL 也为 0 时兜底 3 分钟
	assert.Equal(t, 3*time.Minute, resolveRefreshInterval(AuthConfig{}))
}
