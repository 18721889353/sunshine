package dlock

import (
	"context"
	"errors"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// 默认的会话过期时间（秒）
var defaultTTL = 15 // seconds

// EtcdLock 结构体表示一个基于 etcd 的分布式锁
type EtcdLock struct {
	session *concurrency.Session // etcd 会话
	mutex   *concurrency.Mutex   // 分布式互斥锁
}

// NewEtcd 创建一个新的 etcd 分布式锁
// 参数：
// - client: etcd 客户端实例，不能为空
// - key: 锁的键，不能为空
// - ttl: 会话的过期时间（秒），如果小于等于 0 则使用默认值
// 返回值：
// - Locker: 分布式锁接口实例
// - error: 如果创建失败则返回错误
func NewEtcd(client *clientv3.Client, key string, ttl int) (Locker, error) {
	if client == nil {
		return nil, errors.New("etcd client is nil")
	}

	if key == "" {
		return nil, errors.New("key is empty")
	}

	if ttl <= 0 {
		ttl = defaultTTL
	}

	// 创建一个新的 etcd 会话
	session, err := concurrency.NewSession(
		client,
		concurrency.WithTTL(ttl),
	)
	if err != nil {
		return nil, err
	}
	// 创建一个新的分布式互斥锁
	mutex := concurrency.NewMutex(session, key)

	locker := &EtcdLock{
		session: session,
		mutex:   mutex,
	}

	return locker, nil
}

// Lock 阻塞直到获取到锁或上下文被取消
// 参数：
// - ctx: 上下文，用于控制锁的获取操作
// 返回值：
// - error: 如果获取锁失败则返回错误
func (l *EtcdLock) Lock(ctx context.Context) error {
	return l.mutex.Lock(ctx)
}

// Unlock 释放锁
// 参数：
// - ctx: 上下文，用于控制锁的释放操作
// 返回值：
// - error: 如果释放锁失败则返回错误
func (l *EtcdLock) Unlock(ctx context.Context) error {
	return l.mutex.Unlock(ctx)
}

// TryLock 尝试获取锁而不阻塞
// 参数：
// - ctx: 上下文，用于控制锁的获取操作
// 返回值：
// - bool: 如果成功获取锁则返回 true，否则返回 false
// - error: 如果发生错误则返回错误
func (l *EtcdLock) TryLock(ctx context.Context) (bool, error) {
	err := l.mutex.TryLock(ctx)
	if err == nil {
		return true, nil
	}
	if err == concurrency.ErrLocked {
		return false, nil
	}
	return false, err
}

// Close 释放锁并关闭 etcd 会话
// 返回值：
// - error: 如果关闭会话失败则返回错误
func (l *EtcdLock) Close() error {
	if l.session != nil {
		return l.session.Close()
	}
	return nil
}
