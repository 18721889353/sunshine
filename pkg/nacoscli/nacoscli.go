// Package nacoscli 封装 Nacos 配置中心的客户端操作，提供配置获取、实时监听和服务注册能力。
//
// 核心功能：
//   - 配置获取：GetConfig / Client.GetConfig 从 Nacos 拉取配置，支持 context 超时控制。
//   - 实时监听：WatchConfig 封装注册失败自动重试，context 取消时优雅停止。
//     注册成功后，连接维护由 Nacos SDK 内部长轮询负责。
//   - 服务注册与发现：NewNamingClient 创建命名客户端（NamingClient），用于服务注册/注销/发现。
//   - 选项模式：通过 Option 函数（WithIPAddr、WithAuth、WithClientConfig 等）灵活配置，
//     支持单字段设置与完整 SDK 配置两种方式，优先级：完整配置 > 单字段选项 > 默认值。
//
// 使用方式：
//   - 读取配置：nacoscli.GetConfig(params, nacoscli.WithAuth(user, pass))
//   - 监听变更：nacoscli.WatchConfig(ctx, params, handler, opts...)
//   - 服务注册：nacoscli.NewNamingClient(ip, port, namespaceID, opts...)
//
// 设计说明 — ListenConfig 的非阻塞语义：
//
// Nacos SDK 的 ListenConfig 是非阻塞调用：它仅将回调注册到 SDK 内部的 cacheMap，
// 然后立即返回。真正的长轮询由 SDK 的 startInternal() 后台 goroutine 执行，
// SDK 自己负责运行期的连接维护与重连。
//
// 因此本包的职责边界是：
//   - WatchConfig 负责「注册动作」的失败重试（如地址不可达、认证失败）
//   - 注册成功后，连接健康维护完全由 SDK 内部处理，本包不再介入
//   - 不存在「连接断开后重连」这条路径——SDK 内部自行处理
//
// 这一认知是理解本包 API 设计的关键：
//   - 为何没有「断线重连」相关配置——SDK 内部自行处理
//   - 为何 retries 只在注册失败时递增——不存在「运行期断连」事件
//
// 详见 README.md 中的「重试语义」章节。
package nacoscli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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

// ErrNilParams 当传入 nil *Params 时返回的错误。
var ErrNilParams = errors.New("Params 不能为空")

// Params 包含 Nacos 配置的查询参数。
type Params struct {
	IPAddr      string `yaml:"ipAddr" json:"ipAddr"`           // 服务器地址
	Port        int    `yaml:"port" json:"port"`               // 端口，未提供 WithServerConfigs 时必填
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
	if len(serverConfigs) == 0 {
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

// valid 检查 Params 结构体中的必填字段是否有效，并返回归一化后的 Format。
// 不修改原始 Params 结构体，避免隐式副作用。
func (p *Params) valid() (string, error) {
	if p.Group == "" {
		return "", errors.New("字段 'Group' 不能为空")
	}
	if p.DataID == "" {
		return "", errors.New("字段 'DataID' 不能为空")
	}
	if p.Format == "" {
		return "", errors.New("字段 'Format' 不能为空")
	}
	format := strings.ToLower(p.Format)
	switch format {
	case "json", "yaml", "toml":
	case "yml":
		format = "yaml"
	default:
		return "", fmt.Errorf("配置文件类型 'Format=%s' 不支持", p.Format)
	}

	return format, nil
}

// ---------------------------------------------------------------------------
// Client - Nacos 配置客户端（带生命周期管理）
// ---------------------------------------------------------------------------

// Client 是 Nacos 配置客户端，封装了配置客户端的创建、复用与销毁。
type Client struct {
	configClient config_client.IConfigClient
}

// NewConfigClient 创建一个 Nacos 配置客户端。
//
// 支持以下配置方式（优先级从高到低）：
//  1. WithClientConfig / WithServerConfigs — 完全自定义 SDK 配置
//  2. 单字段 Option（WithIPAddr、WithNamespaceID、WithAuth 等）
//  3. 默认值（timeoutMs=5000, LogDir/CacheDir 使用系统临时目录）
func NewConfigClient(opts ...Option) (*Client, error) {
	o := defaultOptions()
	o.apply(opts...)

	if o.ipAddr == "" && len(o.serverConfigs) == 0 {
		return nil, errors.New("Nacos 服务器地址 (IPAddr/IP 或 WithIPAddr) 不能为空")
	}
	if o.port == 0 && len(o.serverConfigs) == 0 {
		return nil, errors.New("Nacos 服务器端口 (Port 或 WithPort) 不能为空")
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

// GetConfig 从 Nacos 获取配置，支持通过 context 传递超时与取消信号。
// 内部会校验 params 的 nil 与必填字段，非法参数返回错误，不会 panic。
func (c *Client) GetConfig(ctx context.Context, params *Params) (format string, content []byte, err error) {
	if params == nil {
		return "", nil, ErrNilParams
	}

	tracer := otel.Tracer("nacoscli")
	ctx, span := tracer.Start(ctx, "nacos.get_config", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	span.SetAttributes(
		attribute.String("nacos.data_id", params.DataID),
		attribute.String("nacos.group", params.Group),
	)
	if reqID := requestIDAttr(ctx); reqID != nil {
		span.SetAttributes(*reqID)
	}

	// 先检查 ctx 再校验参数，避免已取消时做无意义校验
	select {
	case <-ctx.Done():
		err = ctx.Err()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", nil, err
	default:
	}

	normalizedFormat, err := params.valid()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", nil, err
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
	span.SetStatus(codes.Ok, "配置获取成功")
	return normalizedFormat, []byte(data), nil
}

// Close 关闭 Nacos 配置客户端，释放底层连接资源。
// Nacos SDK 的 CloseClient() 不返回错误，此处直接调用。
// 安全措施：若 configClient 为 nil（未正常初始化），直接返回。
func (c *Client) Close() {
	if c.configClient == nil {
		return
	}
	c.configClient.CloseClient()
}

// ---------------------------------------------------------------------------
// 便捷函数（向后兼容）
// ---------------------------------------------------------------------------

// GetConfig 从 Nacos 配置中心获取配置并返回配置内容与格式。
// 每次调用都会创建并销毁一个客户端，高频场景建议使用 NewConfigClient 复用连接。
// 超时时间通过 WithGetTimeout 配置（默认 30 秒）。
func GetConfig(params *Params, opts ...Option) (string, []byte, error) {
	if params == nil {
		return "", nil, ErrNilParams
	}
	if _, err := params.valid(); err != nil {
		return "", nil, err
	}

	o := defaultOptions()
	o.apply(opts...)

	baseOpts := []Option{
		WithIPAddr(params.IPAddr),
		WithPort(params.Port),
		WithScheme(params.Scheme),
		WithContextPath(params.ContextPath),
		WithNamespaceID(params.NamespaceID),
	}
	mergedOpts := append([]Option{}, baseOpts...)
	mergedOpts = append(mergedOpts, opts...)

	client, err := NewConfigClient(mergedOpts...)
	if err != nil {
		return "", nil, err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), o.getTimeout)
	defer cancel()

	format, data, err := client.GetConfig(ctx, params)
	return format, data, err
}

// NewNamingClient 创建一个 Nacos 服务注册与发现客户端。
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

	return clients.NewNamingClient(
		vo.NacosClientParam{
			ClientConfig:  clientConfig,
			ServerConfigs: serverConfigs,
		},
	)
}

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
// 若 context 中不存在 request_id，返回 nil（不设置空字符串属性）。
func requestIDAttr(ctx context.Context) *attribute.KeyValue {
	if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
		v := attribute.String("nacoscli.request_id", reqID)
		return &v
	}
	return nil
}
