package nacoscli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

// Params 包含 Nacos 的配置参数。
type Params struct {
	IPAddr      string // 服务器地址
	Port        uint64 // 端口
	Scheme      string // 协议，http 或 grpc
	ContextPath string // 路径
	// 如果设置了此参数，上述字段（IPAddr, Port, Scheme, ContextPath）将无效
	serverConfigs []constant.ServerConfig

	NamespaceID string // 命名空间 ID
	// 如果设置了此参数，上述字段（NamespaceID）将无效
	clientConfig *constant.ClientConfig

	Group  string // 分组，例如：dev, prod, test
	DataID string // 配置文件 ID
	Format string // 配置文件类型：json, yaml, toml
}

// valid 检查 Params 结构体中的必填字段是否有效。
func (p *Params) valid() error {
	if p.Group == "" {
		return errors.New("字段 'Group' 不能为空")
	}
	if p.DataID == "" {
		return errors.New("字段 'DataID' 不能为空")
	}
	if p.Format == "" {
		return errors.New("字段 'Format' 不能为空")
	}
	format := strings.ToLower(p.Format)
	switch format {
	case "json", "yaml", "toml":
		p.Format = format
	case "yml":
		p.Format = "yaml"
	default:
		return fmt.Errorf("配置文件类型 'Format=%s' 不支持", p.Format)
	}

	return nil
}

// setParams 根据传入的选项设置 Params 结构体中的参数。
func setParams(params *Params, opts ...Option) {
	o := defaultOptions()
	o.apply(opts...)
	params.clientConfig = o.clientConfig
	params.serverConfigs = o.serverConfigs

	// 创建 clientConfig
	if params.clientConfig == nil {
		params.clientConfig = &constant.ClientConfig{
			NamespaceId:         params.NamespaceID,
			TimeoutMs:           5000,
			NotLoadCacheAtStart: true,
			LogDir:              os.TempDir() + "/nacos/log",
			CacheDir:            os.TempDir() + "/nacos/cache",
			Username:            o.username,
			Password:            o.password,
		}
	}

	// 创建 serverConfig
	if params.serverConfigs == nil {
		params.serverConfigs = []constant.ServerConfig{
			{
				IpAddr:      params.IPAddr,
				Port:        params.Port,
				Scheme:      params.Scheme,
				ContextPath: params.ContextPath,
			},
		}
	}
}

// GetConfig 从 Nacos 获取配置并返回配置内容。
func GetConfig(params *Params, opts ...Option) (string, []byte, error) {
	err := params.valid()
	if err != nil {
		return "", nil, err
	}

	setParams(params, opts...)

	// 创建动态配置客户端
	configClient, err := clients.NewConfigClient(
		vo.NacosClientParam{
			ClientConfig:  params.clientConfig,
			ServerConfigs: params.serverConfigs,
		},
	)
	if err != nil {
		return "", nil, err
	}

	// 读取配置内容
	data, err := configClient.GetConfig(vo.ConfigParam{
		DataId: params.DataID,
		Group:  params.Group,
	})
	if err != nil {
		return "", nil, err
	}

	return params.Format, []byte(data), err
}

// Init 从 Nacos 获取配置并解析为结构体，用于配置中心。
//
// 已弃用：请使用 GetConfig 替代。
func Init(_ interface{}, _ *Params, _ ...Option) error {
	return errors.New("未实现，使用 GetConfig 替代")
}

// NewNamingClient 创建一个 Nacos 服务注册与发现客户端。
// 注意：如果设置了参数 WithClientConfig，nacosNamespaceID 将无效，
// 如果设置了参数 WithServerConfigs，nacosIPAddr 和 nacosPort 将无效。
func NewNamingClient(nacosIPAddr string, nacosPort int, nacosNamespaceID string, opts ...Option) (naming_client.INamingClient, error) {
	params := &Params{
		IPAddr:      nacosIPAddr,
		Port:        uint64(nacosPort),
		NamespaceID: nacosNamespaceID,
	}
	setParams(params, opts...)

	return clients.NewNamingClient(
		vo.NacosClientParam{
			ClientConfig:  params.clientConfig,
			ServerConfigs: params.serverConfigs,
		},
	)
}
