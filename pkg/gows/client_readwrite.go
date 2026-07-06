package gows

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
)

// writeLoopRetries 写入失败最大重试次数
const (
	writeLoopRetries = 3                // 写入失败最大重试次数
	writeDeadline    = 10 * time.Second // 默认单次写入超时时间
)

// msgFromWsToCh 从底层 WebSocket 连接接收消息并推入 readCh 供 ReadMsgFromClientReadCh 消费。
// 通过 readCh 缓冲通道解耦网络读取和业务处理，满队列时反压到 TCP 读取层。
// 支持主动读超时保护（独立于心跳），超时或读取失败时记录错误并关闭底层连接。
//
// 核心机制:
//   - readTimeout > 0 时每次读前设置 SetReadDeadline，超时后 ReadMessage 返回 timeout 错误
//   - 读取成功后阻塞推入 readCh，队列满时反压到 TCP 读取层，不丢弃消息
//   - 读取失败后调用 closeWsConn 关闭底层 TCP 连接（不自锁），不调用 Close
//   - 退出时 defer 关闭 readCh，触发 ReadMsgFromClientReadCh 的 <-c.readCh 返回 !ok
//
// 退出路径:
//   - ReadMessage 返回错误（网络断开/超时/连接关闭）→ closeWsConn → return
//   - SetReadDeadline 失败（连接已不可用）→ closeWsConn → return
//   - clientCtx 被取消（Close 调用）→ return（阻塞推入时响应）
//
// 注意:
//   - 读取失败不重试，直接关闭连接由客户端发起重连
//   - 使用 closeWsConn 而非 Close 避免 msgFromChToWs 自锁
func (c *Client) msgFromWsToCh() {
	defer func() {
		if r := recover(); r != nil {
			c.recordReadErr(fmt.Errorf("panic: %v", r))
			logger.WarnWithCtx(c.clientCtx, "ws msgFromWsToCh panic recovered",
				logger.String("uid", c.uid),
				logger.String("remote_addr", c.remoteAddr),
				logger.Any("panic", r),
			)
		}
		close(c.readCh)
		c.readWg.Done()
	}()

	for {
		// 主动读超时保护（独立于心跳，不依赖 PongHandler）
		// readTimeout=0 表示清除 deadline（不限制）
		var deadline time.Time
		if c.readTimeout > 0 {
			deadline = time.Now().Add(c.readTimeout)
		}
		if err := c.wsConn.SetReadDeadline(deadline); err != nil {
			c.recordReadErr(err)
			c.closeWsConn()
			return
		}

		_, data, err := c.wsConn.ReadMessage()
		if err != nil {
			c.recordReadErr(err)
			c.closeWsConn()
			return
		}

		// 阻塞推入 readCh，队列满时反压到 TCP 读取层，不丢弃消息
		// 关闭时通过 clientCtx.Done() 退出，不永久阻塞
		select {
		case c.readCh <- data:
			c.numReceived.Add(1)
			c.markLastRead()
		case <-c.clientCtx.Done():
			return
		}
	}
}

// msgFromChToWs 从 writeCh 中逐条消费数据并通过底层连接写入网络。
// 网络写入失败时进行最多 writeLoopRetries 次指数退避重试：
//   - 第 1 次重试等待 100ms
//   - 第 2 次重试等待 200ms
//   - 第 3 次重试等待 400ms
//
// 全部重试失败后调用 closeWsConn 关闭底层 TCP 连接，
// 触发消息读取方 ReadMessage 返回错误，进而由调用方执行 Close 完整清理。
// 使用 closeWsConn 而非直接调用 Close 避免 msgFromChToWs 自锁。
func (c *Client) msgFromChToWs() {
	defer func() {
		if r := recover(); r != nil {
			c.recordWriteErr(fmt.Errorf("panic: %v", r))
			logger.WarnWithCtx(c.clientCtx, "ws msgFromChToWs panic recovered",
				logger.String("uid", c.uid),
				logger.String("remote_addr", c.remoteAddr),
				logger.Any("panic", r),
			)
		}
		c.writeWg.Done()
	}()

	for {
		select {
		case data, ok := <-c.writeCh:
			if !ok {
				return
			}
			if err := c.writeWithRetry(data); err != nil {
				logger.WarnWithCtx(c.clientCtx, "ws client write failed after retries, force closing",
					logger.String("uid", c.uid),
					logger.String("remote_addr", c.remoteAddr),
					logger.Int("retries", writeLoopRetries),
					logger.Err(err),
				)
				c.closeWsConn()
				return
			}
			c.numSent.Add(1)
			c.markLastWrite()
		case <-c.clientCtx.Done():
			// 收到关闭信号，drain writeCh 剩余消息确保不丢失
			for len(c.writeCh) > 0 {
				if err := c.writeWithRetry(<-c.writeCh); err != nil {
					break
				}
			}
			return
		}
	}
}

// writeWithRetry 带指数退避 + 随机 jitter 的写入操作。
// 每次写入前设置 10 秒 WriteDeadline，防止网络卡死。
// 退避公式: baseDelay × 2^(attempt-1) + jitter(0~baseDelay)，
// jitter 随机化防止惊群效应。
// 参数:
//   - data: 待写入的序列化字节数据
//
// 返回:
//   - error: 所有重试均失败时返回最后一次错误
func (c *Client) writeWithRetry(data []byte) error {
	var lastErr error
	msgType := c.writeMsgType
	for attempt := 0; attempt <= writeLoopRetries; attempt++ {
		if attempt > 0 {
			// 指数退避：100ms, 200ms, 400ms
			baseDelay := time.Duration(100*math.Pow(2, float64(attempt-1))) * time.Millisecond
			// 随机 jitter: [0, baseDelay) 范围，防止惊群效应
			jitter := time.Duration(rand.Int64N(int64(baseDelay)))
			time.Sleep(baseDelay + jitter)
		}

		// 设置写入超时，防止 TCP 半连接导致永久阻塞
		// NewClient 已确保 writeTimeout > 0（默认 10s）
		if err := c.wsConn.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
			lastErr = err
			c.recordWriteErr(err)
			continue
		}

		if err := c.wsConn.WriteMessage(msgType, data); err != nil {
			lastErr = err
			c.recordWriteErr(err)
			continue
		}
		return nil
	}
	return fmt.Errorf("write failed after %d retries: %w", writeLoopRetries, lastErr)
}

// checkWriteLimit 检查写入数据大小是否超过 writeLimit 限制。
func (c *Client) checkWriteLimit(data []byte) error {
	if c.writeLimit > 0 && len(data) > int(c.writeLimit) {
		return ErrWriteLimitExceeded
	}
	return nil
}
