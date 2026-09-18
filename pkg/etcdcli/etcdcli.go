// Package etcdcli 用于连接到 etcd 服务
package etcdcli

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"
	"unsafe"

	clientv3 "go.etcd.io/etcd/client/v3"
	etcdcredentials "go.etcd.io/etcd/client/v3/credentials"
	"google.golang.org/grpc"
	grpccredentials "google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/18721889353/sunshine/pkg/logger"
)

// refreshCancels 存储每个客户端的 token 刷新取消函数，用于优雅停止。
var refreshCancels sync.Map // map[*clientv3.Client]context.CancelFunc

// StopTokenRefresh 停止指定客户端的 JWT token 自动刷新。
// 通常在客户端 Close() 之前或之后调用，释放后台协程资源。
func StopTokenRefresh(cli *clientv3.Client) {
	if cli == nil {
		return
	}
	if cancel, ok := refreshCancels.LoadAndDelete(cli); ok {
		if fn, ok := cancel.(context.CancelFunc); ok {
			fn()
		}
	}
}

// NewClient 创建一个 etcd 客户端连接。
// 注意: 如果设置了 WithConfig(*clientv3.Config) 参数，则 endpoints 参数将被忽略！
func NewClient(endpoints []string, opts ...Option) (*clientv3.Client, error) {
	// 初始化默认选项
	o := defaultOptions()
	// 应用传递的选项
	o.apply(opts...)

	// 如果配置不为空，直接使用配置创建客户端
	if o.config != nil {
		cli, err := clientv3.New(*o.config)
		if err != nil {
			return nil, fmt.Errorf("clientv3.New(*o.config) connecting to the etcd service error: %v", err)
		}
		// 启动 JWT token 自动刷新（如果配置了 authConfig）
		if o.authConfig != nil && o.config.Username != "" {
			startTokenRefresh(cli, o.config.Username, o.config.Password, *o.authConfig)
		}
		return cli, nil
	}

	// 检查 endpoints 是否为空
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("etcd endpoints cannot be empty")
	}

	// 配置 etcd 客户端
	conf := clientv3.Config{
		Endpoints:            endpoints,
		DialTimeout:          o.dialTimeout,
		DialKeepAliveTime:    o.dialKeepAliveTime,
		DialKeepAliveTimeout: o.dialKeepAliveTimeout,
		AutoSyncInterval:     o.autoSyncInterval,
		Username:             o.username,
		Password:             o.password,
	}

	// 根据是否启用安全模式设置 gRPC 的传输凭证
	if !o.isSecure {
		conf.DialOptions = append(conf.DialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else if o.caFile != "" {
		// 双向 TLS（mTLS）
		tlsCert, err := grpccredentials.NewClientTLSFromFile(o.caFile, o.serverNameOverride)
		if err != nil {
			return nil, fmt.Errorf("NewClientTLSFromFile(caFile) error: %v", err)
		}
		conf.DialOptions = append(conf.DialOptions, grpc.WithTransportCredentials(tlsCert))
	} else {
		// 单向 TLS
		cred, err := grpccredentials.NewClientTLSFromFile(o.certFile, o.serverNameOverride)
		if err != nil {
			return nil, fmt.Errorf("NewClientTLSFromFile error: %v", err)
		}
		conf.DialOptions = append(conf.DialOptions, grpc.WithTransportCredentials(cred))
	}

	// 创建 etcd 客户端
	cli, err := clientv3.New(conf)
	if err != nil {
		return nil, fmt.Errorf("clientv3.New(conf) connecting to the etcd service error: %v", err)
	}

	// 启动 JWT token 自动刷新（如果配置了 authConfig）
	if o.authConfig != nil && o.username != "" {
		startTokenRefresh(cli, o.username, o.password, *o.authConfig)
	}

	return cli, nil
}

// startTokenRefresh 在后台协程中定期主动刷新 etcd JWT token，
// 避免 token 过期后服务端出现 "token is expired" 告警。
// 通过 sync.Map 存储取消函数，调用 StopTokenRefresh(cli) 可优雅停止。
func startTokenRefresh(cli *clientv3.Client, username, password string, cfg AuthConfig) {
	refreshInterval := resolveRefreshInterval(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	refreshCancels.Store(cli, cancel)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				// 防止 panic 导致进程崩溃，记录后静默退出
				logger.WarnWithCtx(ctx, fmt.Sprintf("etcd token refresh panic: %v", r))
			}
		}()

		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				resp, err := cli.Authenticate(ctx, username, password)
				if err != nil {
					logger.WarnWithCtx(ctx, "etcd token refresh failed", logger.String("error", err.Error()))
					continue
				}
				// Authenticate 只返回新 token，不会更新 SDK 内部的 authTokenBundle。
				// SDK 在初始化时通过 getToken() 将 token 存入 authTokenBundle，
				// 后续 gRPC 请求通过 authTokenBundle.PerRPCCredentials() 携带 token。
				// 必须手动调用 UpdateAuthToken 将新 token 写入 SDK 内部，否则请求仍然使用旧 token。
				if !updateClientAuthToken(cli, resp.Token) {
					logger.WarnWithCtx(ctx, "etcd token refresh: failed to update internal auth token bundle")
				}
			}
		}
	}()
}

// resolveRefreshInterval 根据 AuthConfig 计算最终的刷新间隔。
// 优先使用用户指定的 RefreshInterval，否则取 TokenTTL 的 60%，兜底 3 分钟。
func resolveRefreshInterval(cfg AuthConfig) time.Duration {
	if cfg.RefreshInterval > 0 {
		return cfg.RefreshInterval
	}
	if cfg.TokenTTL > 0 {
		return time.Duration(float64(cfg.TokenTTL) * 0.6)
	}
	return 3 * time.Minute
}

// updateClientAuthToken 通过 unsafe 更新 etcd 客户端 SDK 内部的 authTokenBundle。
//
// etcd client v3 在初始化时，如果 Config.Username/Password 非空，会：
//   - 创建 authTokenBundle = credentials.NewPerRPCCredentialBundle()
//   - 调用 getToken() -> cli.Auth.Authenticate() -> authTokenBundle.UpdateAuthToken(token)
//
// 后续每次 gRPC 请求，SDK 通过 authTokenBundle.PerRPCCredentials() 携带最新 token。
// 但 cli.Authenticate() 只返回新 token，不会更新 authTokenBundle，
// 因此需要通过 unsafe 手动调用 UpdateAuthToken 将新 token 写入 SDK 内部。
//
// 为什么不用 reflect：authTokenBundle 是未导出字段，reflect.Value.Interface()
// 在 Go 1.17+ 会对未导出字段 panic，而该 panic 会被 startTokenRefresh 的
// recover() 吞掉，导致 token 刷新静默失败。
//
// 返回 true 表示更新成功，false 表示字段不存在或类型不匹配。
func updateClientAuthToken(cli *clientv3.Client, token string) bool {
	// 获取 Client 结构体指针
	cliPtr := unsafe.Pointer(cli)

	// 使用 reflect 计算 authTokenBundle 字段的偏移量（安全且不依赖硬编码）
	t := reflect.TypeOf(clientv3.Client{})
	field, ok := t.FieldByName("authTokenBundle")
	if !ok {
		return false
	}
	offset := field.Offset

	// 通过 unsafe 指针偏移获取 authTokenBundle 接口值的地址
	bundlePtr := unsafe.Pointer(uintptr(cliPtr) + offset)

	// 将内存地址转换为接口值指针，再解引用获取实际的接口值
	bundle := *(*etcdcredentials.PerRPCCredentialsBundle)(bundlePtr)
	if bundle == nil {
		return false
	}

	bundle.UpdateAuthToken(token)
	return true
}
