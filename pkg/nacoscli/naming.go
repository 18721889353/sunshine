package nacoscli

import (
	"errors"
	"fmt"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

// NewNamingClient 创建 Nacos 命名客户端（服务注册与发现 SDK 客户端的统一工厂）。
//
// 职责边界：本函数仅负责按统一连接参数（地址 / 命名空间 / 认证 / 超时）创建 SDK 客户端，
// 不封装注册与发现的业务语义；服务注册与发现的领域逻辑（注册/注销、防误删校验、
// 指数退避重注册、watcher、OpenTelemetry 追踪）统一由 pkg/servicerd 实现
// （见 servicerd/registry/nacos），本包不重复封装。
//
// 适用边界：返回 SDK 接口类型意味着调用方与 nacos-sdk-go 的 API 稳定性耦合，
// 这在本仓 mono-repo 内成立（nacoscli 与 servicerd 同仓，SDK 升级时一起改）；
// 若将来作为独立库对外发布，应改回封装类型以隔离 SDK 的破坏性变更。
//
// 参数:
//   - nacosIPAddr: Nacos 服务器地址，当 opts 中指定 WithServerConfigs 时被覆盖。
//   - nacosPort: Nacos 服务器端口，当 opts 中指定 WithServerConfigs 时被覆盖。
//   - nacosNamespaceID: Nacos 命名空间 ID，当 opts 中指定 WithClientConfig 时被覆盖。
//   - opts: 连接配置选项，优先级高于前三项参数。
//
// 返回值:
//   - naming_client.INamingClient: Nacos SDK 命名客户端接口，
//     可直接注入 servicerd/registry/nacos.New 用于服务注册与发现。
//     返回的是 SDK 接口类型（工厂定位）：生命周期由调用方负责，用完调用 CloseClient()。
//   - error: 创建客户端过程中的错误。
func NewNamingClient(nacosIPAddr string, nacosPort int, nacosNamespaceID string, opts ...Option) (naming_client.INamingClient, error) {
	baseOpts := []Option{
		WithIPAddr(nacosIPAddr),
		WithPort(nacosPort),
		WithNamespaceID(nacosNamespaceID),
	}
	mergedOpts := append([]Option{}, baseOpts...)
	mergedOpts = append(mergedOpts, opts...)

	o := defaultOptions()
	o.apply(mergedOpts...)

	// 显式校验地址配置，与 NewConfigClient 保持一致
	if o.ipAddr == "" && len(o.serverConfigs) == 0 {
		return nil, errors.New("Nacos 服务器地址 (IPAddr/IP 或 WithIPAddr/WithServerConfigs) 不能为空")
	}
	if o.port == 0 && len(o.serverConfigs) == 0 {
		return nil, errors.New("Nacos 服务器端口 (Port 或 WithPort/WithServerConfigs) 不能为空")
	}

	clientConfig, serverConfigs := buildConfigs(o)

	namingClient, err := clients.NewNamingClient(
		vo.NacosClientParam{
			ClientConfig:  clientConfig,
			ServerConfigs: serverConfigs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("创建 Nacos 命名客户端失败: %w", err)
	}

	return namingClient, nil
}
