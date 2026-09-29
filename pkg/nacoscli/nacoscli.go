// Package nacoscli 封装 Nacos 配置中心的客户端操作，提供配置获取、实时监听和服务注册能力。
//
// 核心功能：
//   - 配置获取：GetConfig / Client.GetConfig 从 Nacos 拉取配置，支持 context 超时控制
//     （内部通过 goroutine + select 实现，ctx 取消/超时会立即返回）。
//   - 实时监听：WatchConfig 封装注册失败自动重试，context 取消时优雅停止。
//     注册成功后，连接维护由 Nacos SDK 内部长轮询负责。
//   - 服务注册与发现：NewNamingClient 按本包统一的连接参数创建 SDK 命名客户端
//     （naming_client.INamingClient），仅承担工厂职责；注册/发现的业务语义与追踪埋点
//     由 pkg/servicerd 实现，本包不再封装注册/发现方法。
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
	"unicode/utf8"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
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
//
// 注意：WatchConfig 会对 *Params 做浅拷贝（见 watch.go 的 paramsCopy）以避免与调用方数据竞争，
// 因此本结构体新增字段必须是值类型（string/int 等）；
// 若必须新增 slice/map/pointer 字段，需同步将 watch.go 的浅拷贝改为深拷贝，否则会引入数据竞争。
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
		// Format 来自配置文件/配置中心，用 %q 而非 %s：避免其中的换行/控制字符被原样带入日志与错误消息（日志注入）
		return "", fmt.Errorf("配置文件类型 'Format=%q' 不支持", p.Format)
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
	return newClientFromOptions(o)
}

// newClientFromOptions 按已解析的 options 创建配置客户端，供 NewConfigClient 与 NewListenClient 复用。
func newClientFromOptions(o *options) (*Client, error) {
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

// GetConfig 从 Nacos 获取配置，通过 context 控制超时与取消。
// 内部会校验 params 的 nil 与必填字段，非法参数返回错误，不会 panic。
//
// 由于 Nacos SDK 的 GetConfig 不接受 context，内部将 SDK 调用放入独立 goroutine，
// 再通过 select 等待 ctx 或结果：ctx 取消/超时时立即返回 ctx.Err()。
// 注意 trade-off：超时返回后，后台 goroutine 仍会运行至 SDK 自身超时（WithTimeoutMs，
// 默认 5000ms）才退出，期间无法被取消；结果写入带缓冲 channel，不会阻塞泄漏。
func (c *Client) GetConfig(ctx context.Context, params *Params) (string, []byte, error) {
	if params == nil {
		return "", nil, ErrNilParams
	}

	tracer := otel.Tracer("nacoscli")
	ctx, span := tracer.Start(ctx, "nacos.get_config", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// DataID/Group 是外部可控输入（来自配置文件/配置中心），写入 Span 属性前先归一化非法 UTF-8
	span.SetAttributes(
		attribute.String("nacos.data_id", safeAttrText(params.DataID)),
		attribute.String("nacos.group", safeAttrText(params.Group)),
	)
	if attr := requestIDAttr(ctx); attr.Key != "" {
		span.SetAttributes(attr)
	}

	// 先检查 ctx 再校验参数，避免已取消时做无意义校验
	select {
	case <-ctx.Done():
		err := ctx.Err()
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

	// SDK 调用不接受 ctx，通过 goroutine + select 让调用方的取消/超时真正生效
	type getResult struct {
		data string
		err  error
	}
	resultCh := make(chan getResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// recover 值常为 error，用 %w 保留错误链（调用方可 errors.Is/As）；非 error 时退回 %v，文本不变
				if asErr, ok := r.(error); ok {
					resultCh <- getResult{err: fmt.Errorf("GetConfig panic: %w", asErr)}
					return
				}
				resultCh <- getResult{err: fmt.Errorf("GetConfig panic: %v", r)}
			}
		}()
		data, callErr := c.configClient.GetConfig(vo.ConfigParam{
			DataId: params.DataID,
			Group:  params.Group,
		})
		resultCh <- getResult{data: data, err: callErr}
	}()

	select {
	case <-ctx.Done():
		err := ctx.Err()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", nil, err
	case r := <-resultCh:
		if r.err != nil {
			span.RecordError(r.err)
			span.SetStatus(codes.Error, r.err.Error())
			return "", nil, fmt.Errorf("从 Nacos 获取配置失败: %w", r.err)
		}
		span.SetAttributes(attribute.Int("nacos.config_length", len(r.data)))
		span.SetStatus(codes.Ok, "配置获取成功")
		return normalizedFormat, []byte(r.data), nil
	}
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
	// 此处的提前校验与下方 client.GetConfig 内部校验存在一次重复，是有意为之的防御：
	// 非法参数在此直接返回，避免为一个注定失败的请求创建 Nacos 客户端（SDK 会初始化缓存目录与后台 goroutine）。
	// valid() 无副作用（不修改 Params，实测快路径 9ns / 0 分配），因此重复调用不会重复触发任何行为。
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

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
// request_id 的 context key 是全项目约定（由 pkg/logger 定义），本包不再提供注入点，
// 与日志走全局 logger 的选型保持一致；提取不到时返回零值（Key 为空，调用方据此不设置属性，
// 避免上报空值），不以指针返回——本函数在每次 GetConfig 上执行，返回值可避开为「可能不存在的属性」
// 额外堆分配一个 KeyValue。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
		return attribute.String("nacoscli.request_id", reqID)
	}
	return attribute.KeyValue{}
}

// safeAttrText 将外部可控文本归一化为合法 UTF-8，供写入 Span 属性使用。
// OTLP 的 protobuf string 字段要求合法 UTF-8，非法字节会让整批 Span 被后端拒接或渲染成乱码；
// Group/DataID 由配置文件与配置中心而来，不能假设它一定合法 UTF-8（与 pkg/goredis 的 Redis key 同一类风险）。
// 合法输入走 utf8.ValidString 快路径直接返回，不发生拷贝。
func safeAttrText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, string(utf8.RuneError))
}
