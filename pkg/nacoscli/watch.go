package nacoscli

import (
	"context"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
)

// WatchConfig 启动 Nacos 配置监听（后台 goroutine），支持自动重连。
// 连接断开或创建失败时自动等待后重试，context 取消时优雅停止。
//
// 参数:
//   - ctx: 控制监听生命周期，取消时停止监听。
//   - params: 配置查询参数（Group/DataID/Format）。
//   - handler: 配置变更回调函数。
//   - opts: 可选连接选项，优先级高于 params 中的连接参数；
//     支持 WithMaxRetries、WithCreateDelay、WithReconnectDelay 控制重试行为。
//
// 返回值:
//   - context.CancelFunc: 调用后立即停止监听 goroutine。
func WatchConfig(ctx context.Context, params *Params, handler ChangeHandler, opts ...Option) context.CancelFunc {
	watchCtx, cancel := context.WithCancel(ctx)

	// 解析重试配置
	o := defaultOptions()
	o.apply(opts...)

	go func() {
		var retries int
		for {
			listener, err := NewListenClient(params, handler, opts...)
			if err != nil {
				retries++
				logger.WarnWithCtx(watchCtx, "[nacos watch] 创建监听器失败",
					logger.Int("retries", retries),
					logger.Err(err),
				)
				if o.maxRetries > 0 && retries >= o.maxRetries {
					logger.ErrorWithCtx(watchCtx, "[nacos watch] 已达最大重试次数，停止监听",
						logger.Int("maxRetries", o.maxRetries),
					)
					return
				}
				select {
				case <-watchCtx.Done():
					logger.InfoWithCtx(watchCtx, "[nacos watch] 上下文取消，停止监听")
					return
				case <-time.After(o.createDelay):
					continue
				}
			}

			logger.InfoWithCtx(watchCtx, "[nacos watch] 监听器已连接",
				logger.String("dataID", params.DataID),
			)
			listener.Start(watchCtx) // 阻塞直到连接断开或 context 取消
			if closeErr := listener.Close(); closeErr != nil {
				logger.WarnWithCtx(watchCtx, "[nacos watch] 关闭监听器失败",
					logger.Err(closeErr),
				)
			}

			// 检查是否是主动取消
			if watchCtx.Err() != nil {
				logger.InfoWithCtx(watchCtx, "[nacos watch] 上下文取消，停止监听")
				return
			}

			// 连接断开，等待后重连
			retries++
			logger.WarnWithCtx(watchCtx, "[nacos watch] 连接断开，等待重连",
				logger.Int("retries", retries),
			)
			if o.maxRetries > 0 && retries >= o.maxRetries {
				logger.ErrorWithCtx(watchCtx, "[nacos watch] 已达最大重试次数，停止监听",
					logger.Int("maxRetries", o.maxRetries),
				)
				return
			}
			select {
			case <-watchCtx.Done():
				logger.InfoWithCtx(watchCtx, "[nacos watch] 上下文取消，停止监听")
				return
			case <-time.After(o.reconnectDelay):
				continue
			}
		}
	}()

	logger.InfoWithCtx(watchCtx, "[nacos watch] 已启动",
		logger.String("dataID", params.DataID),
		logger.String("group", params.Group),
	)

	return cancel
}
