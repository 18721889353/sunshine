## etcdcli

Connect to the etcd service client.

### Example of use

```go
    import "github.com/18721889353/sunshine/pkg/etcdcli"

    endpoints := []string{"192.168.3.37:2379"}
    // 方式1: 通过选项参数设置
    cli, err := etcdcli.NewClient(endpoints,
        etcdcli.WithDialTimeout(time.Second*2),
        etcdcli.WithAuth("root", "password"),
        // etcdcli.WithAutoSyncInterval(0),
    )

    // 方式2: 直接传入 clientv3.Config
    cli, err = etcdcli.NewClient(nil, etcdcli.WithConfig(&clientv3.Config{
        Endpoints:   endpoints,
        DialTimeout: time.Second * 2,
        // Username:    "",
        // Password:    "",
    }))
```

### JWT Token 自动刷新

当 etcd 服务端使用 JWT 认证时，token 有固定的 TTL（如5分钟）。默认情况下，etcd 客户端 SDK 会在 token 过期后被动刷新（服务端会先拒绝请求再让客户端重新认证），导致服务端日志出现 `"token is expired"` 告警。

启用主动 token 刷新后，客户端会在后台协程中定期续期 token，避免服务端告警。

```go
    // 方式1: 通过 WithAuthConfig 选项启用
    cli, err := etcdcli.NewClient(endpoints,
        etcdcli.WithAuth("root", "password"),
        etcdcli.WithAuthConfig(etcdcli.AuthConfig{
            TokenTTL: 5 * time.Minute, // 服务端 JWT token 有效期
        }),
    )

    // 方式2: 通过 YAML 配置（etcdInfo.etcdClient.authTokenTTL）
    // etcdInfo:
    //   etcdClient:
    //     username: root
    //     password: password
    //     authTokenTTL: 5m  # JWT token 有效期，客户端将自动在过期前刷新
```

`AuthConfig` 字段说明：
- `TokenTTL`: 服务端签发的 JWT token 有效期（如 `5m`）
- `RefreshInterval`: 客户端刷新间隔，默认为 `TokenTTL * 0.6`（即过期前40%时间刷新）
