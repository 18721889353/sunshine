package nacoscli

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
)

// exceededMaxRetries 判断是否已达最大重试次数。
// maxRetries <= 0 表示无限重试，永远返回 false。
func exceededMaxRetries(maxRetries, current int) bool {
	return maxRetries > 0 && current >= maxRetries
}

// WatchConfig 启动 Nacos 配置监听（后台 goroutine），支持注册失败自动重试。
// ListenConfig 注册失败时自动等待后重试，context 取消时优雅停止。
// 注册成功后，连接维护由 Nacos SDK 内部长轮询负责，WatchConfig 不再介入。
//
// 返回值:
//   - context.CancelFunc: 停止监听并等待退出。所有错误路径也返回非 nil 函数，可安全 defer stop()。
//   - error: params 校验失败或 handler 为空时立即返回。
func WatchConfig(ctx context.Context, params *Params, handler ChangeHandler, opts ...Option) (context.CancelFunc, error) {
	// 空停止函数，所有错误路径统一返回，避免调用方 defer stop() panic
	noOpStop := func() {}

	// 前置短路：ctx 已取消时直接返回，避免无谓的 Nacos 调用
	select {
	case <-ctx.Done():
		return noOpStop, ctx.Err()
	default:
	}

	// 提前校验参数，避免无效参数进入后台循环
	if params == nil {
		return noOpStop, ErrNilParams
	}
	if handler == nil {
		return noOpStop, errors.New("配置变更回调函数不能为空")
	}
	if _, err := params.valid(); err != nil {
		return noOpStop, err
	}

	watchCtx, cancel := context.WithCancel(ctx)

	// 拷贝一份 params（当前字段全为值类型，浅拷贝即可），避免与调用方数据竞争
	paramsCopy := *params

	// 解析重试配置
	o := defaultOptions()
	o.apply(opts...)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		var retries int
		for {
			listener, err := NewListenClient(&paramsCopy, handler, opts...)
			if err != nil {
				retries++
				logger.WarnWithCtx(watchCtx, "[nacos watch] 创建监听器失败",
					logger.Int("retries", retries),
					logger.Err(err),
				)
				if exceededMaxRetries(o.maxRetries, retries) {
					logger.ErrorWithCtx(watchCtx, "[nacos watch] 已达最大重试次数，停止监听",
						logger.Int("maxRetries", o.maxRetries),
					)
					return
				}
				select {
				case <-watchCtx.Done():
					return
				case <-time.After(o.createDelay):
					continue
				}
			}

			// Start 返回 nil 表示 ctx 正常取消，返回 error 表示 ListenConfig 注册失败
			startErr := listener.Start(watchCtx)

			if startErr == nil {
				// 正常路径：ctx 取消，CancelListenConfig 返回错误属预期，记录日志但不阻断
				if stopErr := listener.Stop(); stopErr != nil {
					logger.DebugWithCtx(watchCtx, "[nacos watch] ctx 取消后停止监听",
						logger.Err(stopErr),
					)
				}
				listener.Close()
				return
			}

			// 异常路径：ctx 未取消，Stop 失败是真实故障
			if stopErr := listener.Stop(); stopErr != nil {
				logger.WarnWithCtx(watchCtx, "[nacos watch] 取消监听注册失败",
					logger.Err(stopErr),
				)
			}
			listener.Close()

			// 累加重试计数，尝试重试
			retries++
			logger.WarnWithCtx(watchCtx, "[nacos watch] 监听注册失败，等待重试",
				logger.Int("retries", retries),
				logger.Err(startErr),
			)
			if exceededMaxRetries(o.maxRetries, retries) {
				logger.ErrorWithCtx(watchCtx, "[nacos watch] 已达最大重试次数，停止监听",
					logger.Int("maxRetries", o.maxRetries),
				)
				return
			}
			select {
			case <-watchCtx.Done():
				return
			case <-time.After(o.createDelay):
				continue
			}
		}
	}()

	logger.InfoWithCtx(watchCtx, "[nacos watch] 已启动",
		logger.String("dataID", paramsCopy.DataID),
		logger.String("group", paramsCopy.Group),
	)

	// 返回可等待的 stop 函数
	stop := func() {
		cancel()
		wg.Wait()
	}
	return stop, nil
}
