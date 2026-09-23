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
// 通过 ListenConfig 注册监听，SDK 内部后台 goroutine 执行长轮询，
// Start 通过 <-ctx.Done() 阻塞等待停止信号，Stop 调用 CancelListenConfig 取消注册。
// 注意：ListenClient 非并发安全，不要在多个 goroutine 中同时调用 Start/Stop/Close。
type ListenClient struct {
	configClient config_client.IConfigClient
	group        string
	dataID       string
	handler      ChangeHandler
	param        vo.ConfigParam // 用于 CancelListenConfig 清理
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
	if _, err := params.valid(); err != nil {
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

	client, err := NewConfigClient(mergedOpts...)
	if err != nil {
		return nil, fmt.Errorf("创建 Nacos 配置监听客户端失败: %w", err)
	}

	return &ListenClient{
		configClient: client.configClient,
		group:        params.Group,
		dataID:       params.DataID,
		handler:      handler,
		param: vo.ConfigParam{
			DataId: params.DataID,
			Group:  params.Group,
		},
	}, nil
}

// Start 启动配置监听，阻塞直到 ctx 取消或 ListenConfig 注册失败。
//
// 返回值:
//   - nil: ctx 被取消，正常退出。
//   - error: ListenConfig 注册失败（如 SDK 内部状态异常）。
//
// 无论返回 nil 还是 error，调用方都应调用 Stop() 与 Close() 释放资源。
// CancelListenConfig 对未注册成功的 (dataId, group) 是无害操作。
func (c *ListenClient) Start(ctx context.Context) error {
	logger.InfoWithCtx(ctx, "[nacos listener] 启动中",
		logger.String("group", c.group),
		logger.String("dataId", c.dataID),
	)

	// ListenConfig 是非阻塞调用：仅注册回调到 SDK 内部 cacheMap，
	// 真正的长轮询由 SDK 的 startInternal() 后台 goroutine 执行。
	c.param.OnChange = c.buildOnChange(ctx)
	if err := c.configClient.ListenConfig(c.param); err != nil {
		logger.WarnWithCtx(ctx, "[nacos listener] 注册监听失败",
			logger.String("dataId", c.dataID),
			logger.Err(err),
		)
		return err
	}

	// 阻塞等待 ctx 取消，保持监听存活
	<-ctx.Done()

	logger.InfoWithCtx(context.Background(), "[nacos listener] 上下文取消，停止监听")
	return nil
}

// Stop 取消配置监听注册，释放 SDK 内部的 cacheData 资源。
// 通常由 WatchConfig 在 Start 返回后调用。
func (c *ListenClient) Stop() error {
	return c.configClient.CancelListenConfig(c.param)
}

// buildOnChange 构造配置变更回调闭包。
func (c *ListenClient) buildOnChange(ctx context.Context) vo.Listener {
	return func(namespace, group, dataId, data string) {
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

// Close 关闭底层 Nacos 配置客户端，释放所有连接资源。
// 关闭前应先调用 Stop() 取消监听注册（对未注册成功的 (dataId, group) 也是安全的）。
// Nacos SDK 的 CloseClient() 不返回错误，此处直接调用。
func (c *ListenClient) Close() {
	if c.configClient == nil {
		return
	}
	c.configClient.CloseClient()
}
