## nacoscli

Nacos 配置中心客户端，支持配置拉取与服务注册发现。

### 功能

- 从 Nacos 配置中心拉取配置（`GetConfig` / `Client.GetConfig`）
- 服务注册与发现客户端创建（`NewNamingClient`）
- 客户端生命周期管理（`NewClient` + `Close`）
- Context 超时取消支持
- 多种认证与连接配置方式

---

### 快速开始

#### 方式一：通过 Params 字段直接配置（向后兼容）

```go
package main

import (
	"fmt"
	"github.com/18721889353/sunshine/pkg/nacoscli"
)

func main() {
	params := &nacoscli.Params{
		IPAddr:      "192.168.3.37",
		Port:        8848,
		NamespaceID: "de7b176e-91cd-49a3-ac83-beb725979775",
		Group:       "dev",
		DataID:      "user-srv.yml",
		Format:      "yaml",
	}
	format, data, err := nacoscli.GetConfig(params)
	if err != nil {
		panic(err)
	}
	fmt.Printf("format: %s, data: %s\n", format, string(data))
}
```

#### 方式二：通过 ClientConfig 和 ServerConfig 选项

```go
package main

import (
	"fmt"
	"os"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/18721889353/sunshine/pkg/nacoscli"
)

func main() {
	params := &nacoscli.Params{
		Group:  "dev",
		DataID: "user-srv.yml",
		Format: "yaml",
	}
	clientConfig := &constant.ClientConfig{
		NamespaceId:         "de7b176e-91cd-49a3-ac83-beb725979775",
		TimeoutMs:           5000,
		NotLoadCacheAtStart: true,
		LogDir:              os.TempDir() + "/nacos/log",
		CacheDir:            os.TempDir() + "/nacos/cache",
	}
	serverConfigs := []constant.ServerConfig{
		{IpAddr: "192.168.3.37", Port: 8848},
	}
	format, data, err := nacoscli.GetConfig(params,
		nacoscli.WithClientConfig(clientConfig),
		nacoscli.WithServerConfigs(serverConfigs),
	)
	if err != nil {
		panic(err)
	}
	fmt.Printf("format: %s, data: %s\n", format, string(data))
}
```

#### 方式三：通过单字段选项（无需 ClientConfig/ServerConfig）

```go
package main

import (
	"fmt"

	"github.com/18721889353/sunshine/pkg/nacoscli"
)

func main() {
	params := &nacoscli.Params{
		Group:  "dev",
		DataID: "user-srv.yml",
		Format: "yaml",
	}
	format, data, err := nacoscli.GetConfig(params,
		nacoscli.WithIPAddr("192.168.3.37"),
		nacoscli.WithPort(8848),
		nacoscli.WithNamespaceID("de7b176e-91cd-49a3-ac83-beb725979775"),
	)
	if err != nil {
		panic(err)
	}
	fmt.Printf("format: %s, data: %s\n", format, string(data))
}
```

---

### 客户端生命周期管理（推荐）

高频获取配置的场景，建议使用 `NewClient` 创建客户端，复用连接。

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/18721889353/sunshine/pkg/nacoscli"
)

func main() {
	client, err := nacoscli.NewClient(
		nacoscli.WithIPAddr("192.168.3.37"),
		nacoscli.WithPort(8848),
		nacoscli.WithNamespaceID("de7b176e-91cd-49a3-ac83-beb725979775"),
		nacoscli.WithTimeoutMs(3000),
	)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	params := &nacoscli.Params{
		Group:  "dev",
		DataID: "user-srv.yml",
		Format: "yaml",
	}

	// 支持 context 超时控制
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	format, data, err := client.GetConfig(ctx, params)
	if err != nil {
		panic(err)
	}
	fmt.Printf("format: %s, data: %s\n", format, string(data))

	// 可复用同一客户端多次获取不同配置
	params2 := &nacoscli.Params{
		Group:  "prod",
		DataID: "another-srv.yml",
		Format: "yaml",
	}
	format2, data2, err := client.GetConfig(context.Background(), params2)
	if err != nil {
		panic(err)
	}
	fmt.Printf("format: %s, data: %s\n", format2, string(data2))
}
```

---

### 身份认证

```go
package main

import (
	"fmt"

	"github.com/18721889353/sunshine/pkg/nacoscli"
)

func main() {
	params := &nacoscli.Params{
		IPAddr:      "192.168.3.37",
		Port:        8848,
		NamespaceID: "de7b176e-91cd-49a3-ac83-beb725979775",
		Group:       "dev",
		DataID:      "user-srv.yml",
		Format:      "yaml",
	}
	format, data, err := nacoscli.GetConfig(params,
		nacoscli.WithAuth("admin", "password"),
	)
	if err != nil {
		panic(err)
	}
	fmt.Printf("format: %s, data: %s\n", format, string(data))
}
```

---

### 服务注册与发现

```go
package main

import (
	"fmt"

	"github.com/18721889353/sunshine/pkg/nacoscli"
)

func main() {
	namingClient, err := nacoscli.NewNamingClient(
		"192.168.3.37",
		8848,
		"de7b176e-91cd-49a3-ac83-beb725979775",
		nacoscli.WithAuth("admin", "password"),
	)
	if err != nil {
		panic(err)
	}
	_ = namingClient
	// 使用 namingClient 进行服务注册与发现
	// 详见: https://github.com/nacos-group/nacos-sdk-go
}
```

---

### Option 列表

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithIPAddr(ip)` | Nacos 服务器地址 | `""` |
| `WithPort(port)` | Nacos 服务器端口 | `0` |
| `WithScheme(scheme)` | 协议（http/grpc） | `""` |
| `WithContextPath(path)` | 上下文路径 | `""` |
| `WithNamespaceID(id)` | 命名空间 ID | `""` |
| `WithTimeoutMs(ms)` | 请求超时（毫秒） | `5000` |
| `WithAuth(user, pass)` | 认证信息 | `""` |
| `WithClientConfig(cfg)` | 完整客户端配置（覆盖以上） | `nil` |
| `WithServerConfigs(cfgs)` | 完整服务器配置（覆盖以上） | `nil` |

> **优先级说明**：`WithClientConfig` 和 `WithServerConfigs` 会覆盖同类别下的所有单字段选项。
