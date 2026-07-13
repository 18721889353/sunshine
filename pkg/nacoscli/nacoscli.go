// Package nacoscli 提供 Nacos 配置中心客户端。
package nacoscli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// Params 包含 Nacos 配置的查询参数。
type Params struct {
	IPAddr      string `yaml:"ipAddr" json:"ipAddr"`           // 服务器地址
	Port        int    `yaml:"port" json:"port"`               // 端口
	Scheme      string `yaml:"scheme" json:"scheme"`           // 协议，http 或 grpc
	ContextPath string `yaml:"contextPath" json:"contextPath"` // 路径
	NamespaceID string `yaml:"namespaceID" json:"namespaceID"` // 命名空间 ID
	Group       string `yaml:"group" json:"group"`             // 分组，例如：dev, prod, test
	DataID      string `yaml:"dataID" json:"dataID"`           // 配置文件 ID
	Format      string `yaml:"format" json:"format"`           // 配置文件类型：json, yaml, toml
}

// buildConfigs 从 options 构建 Nacos SDK 所需的 ClientConfig 和 ServerConfig。
func buildConfigs(o *options) (*constant.ClientConfig, []constant.ServerConfig) {
	clientConfig := o.clientConfig
	if clientConfig == nil {
		clientConfig = &constant.ClientConfig{
			NamespaceId:         o.namespaceID,
			TimeoutMs:           uint64(o.timeoutMs),
			NotLoadCacheAtStart: true,
			LogDir:              os.TempDir() + "/nacos/log",
			CacheDir:            os.TempDir() + "/nacos/cache",
			Username:            o.username,
			Password:            o.password,
		}
	}

	serverConfigs := o.serverConfigs
	if serverConfigs == nil {
		serverConfigs = []constant.ServerConfig{
			{
				IpAddr:      o.ipAddr,
				Port:        uint64(o.port),
				Scheme:      o.scheme,
				ContextPath: o.contextPath,
			},
		}
	}

	return clientConfig, serverConfigs
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

// ---------------------------------------------------------------------------
// Client - Nacos 配置客户端（带生命周期管理）
// ---------------------------------------------------------------------------

// Client 是 Nacos 配置客户端，封装了配置客户端的创建、复用与销毁。
type Client struct {
	configClient config_client.IConfigClient
}

// newConfigClient 创建一个 Nacos 配置客户端。
//
// 支持以下配置方式（优先级从高到低）：
//  1. WithClientConfig / WithServerConfigs — 完全自定义 SDK 配置
//  2. 单字段 Option（WithIPAddr、WithNamespaceID、WithAuth 等）
//  3. 默认值（timeoutMs=5000, LogDir/CacheDir 使用系统临时目录）
func newConfigClient(opts ...Option) (*Client, error) {
	o := defaultOptions()
	o.apply(opts...)

	if o.ipAddr == "" && o.serverConfigs == nil {
		return nil, errors.New("Nacos 服务器地址 (IPAddr/IP 或 WithIPAddr) 不能为空")
	}

	clientConfig, serverConfigs := buildConfigs(o)

	configClient, err := clients.NewConfigClient(
		vo.NacosClientParam{
			ClientConfig:  clientConfig,
			ServerConfigs: serverConfigs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("创建 Nacos 配置客户端失败: %w", err)
	}

	return &Client{configClient: configClient}, nil
}

// getConfig 从 Nacos 获取配置，支持通过 context 传递超时与取消信号。
func (c *Client) getConfig(ctx context.Context, params *Params) (format string, content []byte, err error) {
	tracer := otel.Tracer("nacoscli")
	ctx, span := tracer.Start(ctx, "nacos.get_config", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("nacos.data_id", params.DataID),
		attribute.String("nacos.group", params.Group),
		requestIDAttr(ctx),
	)
	if err = params.valid(); err != nil {
		return "", nil, err
	}

	select {
	case <-ctx.Done():
		return "", nil, ctx.Err()
	default:
	}

	data, err := c.configClient.GetConfig(vo.ConfigParam{
		DataId: params.DataID,
		Group:  params.Group,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", nil, fmt.Errorf("从 Nacos 获取配置失败: %w", err)
	}

	span.SetAttributes(attribute.Int("nacos.config_length", len(data)))
	span.SetStatus(codes.Ok, "config fetched")
	return params.Format, []byte(data), nil
}

// Close 关闭 Nacos 配置客户端，释放底层连接资源。
func (c *Client) Close() error {
	c.configClient.CloseClient()
	return nil
}

// ---------------------------------------------------------------------------
// 便捷函数（向后兼容）
// ---------------------------------------------------------------------------

// GetConfig 从 Nacos 配置中心获取配置并返回配置内容与格式。
func GetConfig(params *Params, opts ...Option) (string, []byte, error) {
	if err := params.valid(); err != nil {
		return "", nil, err
	}

	baseOpts := []Option{
		WithIPAddr(params.IPAddr),
		WithPort(params.Port),
		WithScheme(params.Scheme),
		WithContextPath(params.ContextPath),
		WithNamespaceID(params.NamespaceID),
	}
	mergedOpts := append(baseOpts, opts...)

	client, err := newConfigClient(mergedOpts...)
	if err != nil {
		return "", nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	format, data, err := client.getConfig(ctx, params)
	if closeErr := client.Close(); closeErr != nil && err == nil {
		return "", nil, closeErr
	}
	return format, data, err
}

// NewClient 创建一个 Nacos 服务注册与发现客户端。
//
// 参数:
//   - nacosIPAddr: Nacos 服务器地址，当 opts 中指定 WithServerConfigs 时被覆盖。
//   - nacosPort: Nacos 服务器端口，当 opts 中指定 WithServerConfigs 时被覆盖。
//   - nacosNamespaceID: Nacos 命名空间 ID，当 opts 中指定 WithClientConfig 时被覆盖。
//   - opts: 连接配置选项，优先级高于前三项参数。
//
// 返回值:
//   - naming_client.INamingClient: Nacos 服务注册与发现客户端实例。
//   - error: 创建客户端过程中的错误。
func NewClient(nacosIPAddr string, nacosPort int, nacosNamespaceID string, opts ...Option) (naming_client.INamingClient, error) {
	baseOpts := []Option{
		WithIPAddr(nacosIPAddr),
		WithPort(nacosPort),
		WithNamespaceID(nacosNamespaceID),
	}
	mergedOpts := append(baseOpts, opts...)

	o := defaultOptions()
	o.apply(mergedOpts...)

	clientConfig, serverConfigs := buildConfigs(o)

	return clients.NewNamingClient(
		vo.NacosClientParam{
			ClientConfig:  clientConfig,
			ServerConfigs: serverConfigs,
		},
	)
}

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("nacoscli.request_id", reqID)
		}
	}
	return attribute.String("nacoscli.request_id", "")
}
