package nacoscli

import (
	"context"
	"errors"
	"fmt"

	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ChangeHandler 配置变更回调函数类型。
// 参数为 Nacos 推送的原始配置内容，由调用方决定如何解析。
type ChangeHandler func(namespace, group, dataID, data string)

// ListenClient Nacos 配置监听客户端，封装长生命周期的配置变更订阅。
// 通过 ListenConfig 实现 Nacos 长轮询，配置变更时触发回调。
type ListenClient struct {
	configClient config_client.IConfigClient
	group        string
	dataID       string
	handler      ChangeHandler
}

// NewListenClient 创建配置监听客户端。
//
// 参数:
//   - params: 监听参数，包含 Group、DataID、Format 等必填字段。
//   - handler: 配置变更回调函数，不能为空。
//   - opts: 可选连接配置，优先级高于 params 中的连接参数。
//
// 返回值:
//   - *ListenClient: 监听客户端实例。
//   - error: 创建过程中的错误。
func NewListenClient(params *Params, handler ChangeHandler, opts ...Option) (*ListenClient, error) {
	if handler == nil {
		return nil, errors.New("配置变更回调函数不能为空")
	}
	if params == nil {
		return nil, errors.New("监听参数不能为空")
	}
	if err := params.valid(); err != nil {
		return nil, fmt.Errorf("监听参数校验失败: %w", err)
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
		return nil, fmt.Errorf("创建 Nacos 配置监听客户端失败: %w", err)
	}

	return &ListenClient{
		configClient: client.configClient,
		group:        params.Group,
		dataID:       params.DataID,
		handler:      handler,
	}, nil
}

// Start 启动配置监听（阻塞直到 ctx 取消）。
//
// 内部调用 Nacos SDK 的 ListenConfig，通过长轮询机制接收配置变更推送。
// 监听期间的错误会记录日志但不中断监听（Nacos SDK 内部自动重试）。
//
// 参数:
//   - ctx: 控制监听生命周期的上下文，取消时停止监听并返回。
func (c *ListenClient) Start(ctx context.Context) {
	// 构造 Nacos 监听参数
	param := vo.ConfigParam{
		DataId: c.dataID,
		Group:  c.group,
		OnChange: func(namespace, group, dataId, data string) {
			// context 已取消时丢弃变更
			if ctx.Err() != nil {
				logger.WarnWithCtx(ctx, "[nacos listener] 上下文已取消，跳过配置变更",
					logger.String("group", group),
					logger.String("dataId", dataId),
				)
				return
			}

			logger.InfoWithCtx(ctx, "[nacos listener] 配置已变更",
				logger.String("group", group),
				logger.String("dataId", dataId),
				logger.Int("dataLength", len(data)),
			)

			// 执行用户回调，recover 保护防止 panic 导致监听协程退出
			c.safeCallHandler(ctx, namespace, group, dataId, data)
		},
	}

	logger.InfoWithCtx(ctx, "[nacos listener] 启动中",
		logger.String("group", c.group),
		logger.String("dataId", c.dataID),
	)

	// ListenConfig 是阻塞调用，直到 client 被关闭或 context 取消
	if err := c.configClient.ListenConfig(param); err != nil {
		// 只有主动关闭或 context 取消才会走到这里
		if ctx.Err() != nil {
			logger.InfoWithCtx(ctx, "[nacos listener] 上下文取消，停止监听")
			return
		}
		logger.WarnWithCtx(ctx, "[nacos listener] 监听出错",
			logger.String("dataId", c.dataID),
			logger.Err(err),
		)
	}
}

// safeCallHandler 安全执行配置变更回调，recover 保护防止 panic 影响监听。
func (c *ListenClient) safeCallHandler(ctx context.Context, namespace, group, dataID, data string) {
	defer func() {
		if r := recover(); r != nil {
			logger.WarnWithCtx(ctx, "[nacos listener] 配置变更回调 panic",
				logger.String("dataId", dataID),
				logger.Any("panic", r),
			)
		}
	}()

	c.handler(namespace, group, dataID, data)
}

// Close 关闭监听客户端，释放底层连接资源。
// 注意：Nacos SDK 的 CloseClient() 不返回错误，此处直接调用。
func (c *ListenClient) Close() error {
	if c.configClient == nil {
		return nil
	}
	c.configClient.CloseClient()
	return nil
}
