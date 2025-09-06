package gorabbitmq

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// PoolOption 连接池配置选项函数类型
type PoolOption func(*poolOptions)

// poolOptions 连接池配置选项
type poolOptions struct {
	initialCap int           // 初始连接数
	maxCap     int           // 最大连接数
	maxIdle    time.Duration // 连接最大空闲时间
	zapLog     *zap.Logger   // 日志记录器
	connOpts   []ConnectionOption // 连接选项
}

// apply 应用连接池配置选项
func (o *poolOptions) apply(opts ...PoolOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultPoolOptions 默认连接池配置选项
func defaultPoolOptions() *poolOptions {
	return &poolOptions{
		initialCap: 5,
		maxCap:     30,
		maxIdle:    time.Minute * 10,
		zapLog:     defaultLogger,
		connOpts:   []ConnectionOption{},
	}
}

// WithInitialCap 设置初始连接数
func WithInitialCap(cap int) PoolOption {
	return func(o *poolOptions) {
		if cap > 0 {
			o.initialCap = cap
		}
	}
}

// WithMaxCap 设置最大连接数
func WithMaxCap(cap int) PoolOption {
	return func(o *poolOptions) {
		if cap > 0 {
			o.maxCap = cap
		}
	}
}

// WithMaxIdle 设置连接最大空闲时间
func WithMaxIdle(d time.Duration) PoolOption {
	return func(o *poolOptions) {
		if d > 0 {
			o.maxIdle = d
		}
	}
}

// WithPoolLogger 设置连接池日志记录器
func WithPoolLogger(zapLog *zap.Logger) PoolOption {
	return func(o *poolOptions) {
		if zapLog != nil {
			o.zapLog = zapLog
		}
	}
}

// WithConnOptions 设置连接选项
func WithConnOptions(connOpts ...ConnectionOption) PoolOption {
	return func(o *poolOptions) {
		o.connOpts = connOpts
	}
}

// poolConn 连接池中的连接
type poolConn struct {
	conn       *Connection // RabbitMQ 连接
	createTime time.Time   // 创建时间
	lastUsed   time.Time   // 最后使用时间
}

// Pool 连接池结构
type Pool struct {
	mutex      sync.Mutex     // 互斥锁
	cond       *sync.Cond     // 条件变量
	conns      []*poolConn    // 连接池中的连接列表
	url        string         // 连接 URL
	poolOpts   *poolOptions   // 连接池配置选项
	closed     bool           // 连接池是否已关闭
	totalConns int64          // 原子计数器，跟踪总连接数
}

// NewPool 创建新的连接池
func NewPool(url string, opts ...PoolOption) (*Pool, error) {
	if url == "" {
		return nil, errors.New("url is empty")
	}

	poolOpts := defaultPoolOptions()
	poolOpts.apply(opts...)

	pool := &Pool{
		conns:    make([]*poolConn, 0, poolOpts.maxCap),
		url:      url,
		poolOpts: poolOpts,
	}

	pool.cond = sync.NewCond(&pool.mutex)

	// 初始化连接
	for i := 0; i < poolOpts.initialCap; i++ {
		conn, err := NewConnection(url, poolOpts.connOpts...)
		if err != nil {
			// 关闭已经创建的连接
			pool.Close()
			return nil, err
		}

		pool.conns = append(pool.conns, &poolConn{
			conn:       conn,
			createTime: time.Now(),
			lastUsed:   time.Now(),
		})
		atomic.AddInt64(&pool.totalConns, 1)
	}

	// 启动空闲连接清理协程
	go pool.idleCleanup()

	pool.poolOpts.zapLog.Info("[rabbitmq pool] created successfully",
		zap.String("url", url),
		zap.Int("initialCap", poolOpts.initialCap),
		zap.Int("maxCap", poolOpts.maxCap))

	return pool, nil
}

// Get 从连接池获取一个连接
func (p *Pool) Get() (*Connection, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	for {
		if p.closed {
			return nil, errors.New("pool is closed")
		}

		// 尝试从池中获取一个可用连接
		for i := len(p.conns) - 1; i >= 0; i-- {
			pc := p.conns[i]
			if pc.conn.CheckConnected() {
				// 从池中移除该连接
				p.conns = append(p.conns[:i], p.conns[i+1:]...)
				pc.lastUsed = time.Now()
				p.poolOpts.zapLog.Info("[rabbitmq pool] get existing connection")
				return pc.conn, nil
			}
		}

		// 检查是否可以创建新连接
		if int(atomic.LoadInt64(&p.totalConns)) < p.poolOpts.maxCap {
			// 创建新连接
			conn, err := NewConnection(p.url, p.poolOpts.connOpts...)
			if err != nil {
				return nil, err
			}

			atomic.AddInt64(&p.totalConns, 1)
			p.poolOpts.zapLog.Info("[rabbitmq pool] created new connection")
			return conn, nil
		}

		// 等待连接释放
		p.cond.Wait()
	}
}

// Put 将连接放回连接池
func (p *Pool) Put(conn *Connection) error {
	if conn == nil {
		return nil
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		conn.Close()
		return nil
	}

	// 检查连接是否有效
	if !conn.CheckConnected() {
		atomic.AddInt64(&p.totalConns, -1)
		conn.Close()
		return nil
	}

	// 将连接添加到池中
	pc := &poolConn{
		conn:     conn,
		lastUsed: time.Now(),
	}
	p.conns = append(p.conns, pc)

	p.poolOpts.zapLog.Info("[rabbitmq pool] put connection back to pool",
		zap.Int("poolSize", len(p.conns)))

	// 通知等待的goroutine
	p.cond.Signal()
	return nil
}

// idleCleanup 定期清理空闲连接
func (p *Pool) idleCleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mutex.Lock()

			if p.closed {
				p.mutex.Unlock()
				return
			}

			now := time.Now()
			// 保留的连接数不能少于初始容量
			minKeep := p.poolOpts.initialCap
			if minKeep > len(p.conns) {
				minKeep = len(p.conns)
			}

			// 从后往前遍历，移除空闲时间过长的连接
			for i := len(p.conns) - 1; i >= minKeep; i-- {
				pc := p.conns[i]
				if now.Sub(pc.lastUsed) > p.poolOpts.maxIdle {
					// 关闭连接
					pc.conn.Close()
					atomic.AddInt64(&p.totalConns, -1)

					// 从池中移除
					p.conns = append(p.conns[:i], p.conns[i+1:]...)

					p.poolOpts.zapLog.Debug("[rabbitmq pool] removed idle connection",
						zap.Duration("idleTime", now.Sub(pc.lastUsed)))
				}
			}

			p.mutex.Unlock()
		}
	}
}

// Close 关闭连接池
func (p *Pool) Close() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.closed {
		return
	}

	p.closed = true

	// 关闭所有连接
	for _, pc := range p.conns {
		pc.conn.Close()
		atomic.AddInt64(&p.totalConns, -1)
	}

	p.conns = nil

	// 通知所有等待的goroutine
	p.cond.Broadcast()

	p.poolOpts.zapLog.Info("[rabbitmq pool] closed")
}

// Stats 返回连接池统计信息
func (p *Pool) Stats() map[string]interface{} {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// 计算可用连接数
	available := 0
	for _, pc := range p.conns {
		if pc.conn.CheckConnected() {
			available++
		}
	}

	return map[string]interface{}{
		"totalConns": atomic.LoadInt64(&p.totalConns),
		"available":  available,
		"poolSize":   len(p.conns),
		"maxCap":     p.poolOpts.maxCap,
		"closed":     p.closed,
	}
}