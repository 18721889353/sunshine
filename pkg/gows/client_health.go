package gows

import (
	"sync/atomic"
	"time"
)

// healthState 客户端连接健康状态，使用 atomic.Int64 无锁化时间戳。
// 提取为独立结构体减少 Client 顶层字段数。
type healthState struct {
	lastWriteTime atomic.Int64 // unix nano 最后写入时间
	lastReadTime  atomic.Int64 // unix nano 最后读取时间
	writeErrCount atomic.Int64
	readErrCount  atomic.Int64
	lastWriteErr  atomic.Value
	lastReadErr   atomic.Value
}

// recordWriteErr 原子记录写入错误计数和最近一次错误
func (c *Client) recordWriteErr(err error) {
	c.health.writeErrCount.Add(1)
	c.health.lastWriteErr.Store(err)
}

// recordReadErr 原子记录读取错误计数和最近一次错误
func (c *Client) recordReadErr(err error) {
	c.health.readErrCount.Add(1)
	c.health.lastReadErr.Store(err)
}

// markLastWrite 记录最后一次成功写入时间
func (c *Client) markLastWrite() {
	c.health.lastWriteTime.Store(time.Now().UnixNano())
}

// markLastRead 记录最后一次成功读取时间
func (c *Client) markLastRead() {
	c.health.lastReadTime.Store(time.Now().UnixNano())
}

// IsAlive 判断客户端连接是否处于健康状态。
// 返回 false 的场景：
//   - Close() 已调用
//   - msgFromChToWs 已因写入重试全部失败退出（closeWsConn 已触发）
//   - 超过 3 个心跳周期无成功写入（疑似僵尸连接）
//
// 注意：
//   - 此方法返回 true 不代表底层网络一定可达，仅表示组件内部状态正常
//   - 精确的活性检测依赖 Ping/Pong 协议级心跳 + ReadDeadline 联动
func (c *Client) IsAlive() bool {
	if c.clientIsClosed.Load() {
		return false
	}

	lastWriteNano := c.health.lastWriteTime.Load()

	// 如果从未写入过，视为存活（刚建立的连接）
	if lastWriteNano == 0 {
		return true
	}

	// 超过 3 个心跳周期无成功写入，标记为异常
	if time.Since(time.Unix(0, lastWriteNano)) > 3*defaultHeartbeatInterval {
		return false
	}

	return true
}

// ClientStats 客户端连接统计信息。
type ClientStats struct {
	UID            string // 用户标识
	RemoteAddr     string // 远程地址
	NumSent        int64  // 已发送消息数
	NumReceived    int64  // 已接收消息数
	WriteQueueSize int    // 写入队列容量
	WriteQueueLen  int    // 写入队列当前长度
	ReadQueueSize  int    // 读取队列容量
	ReadQueueLen   int    // 读取队列当前长度
	IsClosed       bool   // 是否已关闭
	IsAlive        bool   // 是否健康（基于 IsAlive() 判断）
	WriteErrCount  int64  // 写入失败累计次数
	ReadErrCount   int64  // 读取失败累计次数
	LastWriteErr   string // 最近一次写入错误信息
	LastReadErr    string // 最近一次读取错误信息
	LastWriteTime  string // 最后一次成功写入时间（ISO8601）
	LastReadTime   string // 最后一次成功读取时间（ISO8601）
}

// Stats 返回客户端连接的实时统计信息。
// 各字段均为 goroutine 安全读取。
func (c *Client) Stats() ClientStats {
	lastWriteNano := c.health.lastWriteTime.Load()
	lastReadNano := c.health.lastReadTime.Load()

	var lastWriteStr, lastReadStr string
	if lastWriteNano != 0 {
		lastWriteStr = time.Unix(0, lastWriteNano).Format(time.RFC3339Nano)
	}
	if lastReadNano != 0 {
		lastReadStr = time.Unix(0, lastReadNano).Format(time.RFC3339Nano)
	}

	var lastWriteErrStr, lastReadErrStr string
	if v := c.health.lastWriteErr.Load(); v != nil {
		if err, ok := v.(error); ok {
			lastWriteErrStr = err.Error()
		}
	}
	if v := c.health.lastReadErr.Load(); v != nil {
		if err, ok := v.(error); ok {
			lastReadErrStr = err.Error()
		}
	}

	return ClientStats{
		UID:            c.uid,
		RemoteAddr:     c.remoteAddr,
		NumSent:        c.numSent.Load(),
		NumReceived:    c.numReceived.Load(),
		WriteQueueSize: cap(c.writeCh),
		WriteQueueLen:  len(c.writeCh),
		ReadQueueSize:  cap(c.readCh),
		ReadQueueLen:   len(c.readCh),
		IsClosed:       c.clientIsClosed.Load(),
		IsAlive:        c.IsAlive(),
		WriteErrCount:  c.health.writeErrCount.Load(),
		ReadErrCount:   c.health.readErrCount.Load(),
		LastWriteErr:   lastWriteErrStr,
		LastReadErr:    lastReadErrStr,
		LastWriteTime:  lastWriteStr,
		LastReadTime:   lastReadStr,
	}
}
