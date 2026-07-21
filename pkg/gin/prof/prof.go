// Package prof 将 pprof 性能分析路由注册到 Gin 引擎。
package prof

import (
	"net/http/pprof"

	"github.com/felixge/fgprof"
	"github.com/gin-gonic/gin"
)

const (
	// DefaultPrefix 默认 pprof 路由前缀
	DefaultPrefix = "/debug/pprof"
)

// Option 定义 Gin pprof 注册的配置选项函数
type Option func(o *options)

type options struct {
	prefix           string
	enableIOWaitTime bool
	authMw           gin.HandlerFunc // 鉴权中间件，nil 表示不启用
}

func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithPrefix 设置 pprof 路由前缀。
// 如果 prefix 为空字符串，使用默认前缀 /debug/pprof。
func WithPrefix(prefix string) Option {
	return func(o *options) {
		if prefix != "" {
			o.prefix = prefix
		}
	}
}

// WithIOWaitTime 启用 IO 等待时间 profile 分析。
// 开启后在 {prefix}/profile-io 路由提供 fgprof 分析，
// 它比标准 pprof 多包含 IO 等待时间，更适合分析 IO 密集型服务。
func WithIOWaitTime() Option {
	return func(o *options) {
		o.enableIOWaitTime = true
	}
}

// WithAuth 设置 pprof 路由的 Gin 鉴权中间件。
// 生产环境建议添加鉴权，防止敏感信息泄露。
//
// 示例:
//
//	prof.Register(r,
//	    prof.WithIOWaitTime(),
//	    prof.WithAuth(gin.BasicAuth(gin.Accounts{
//	        "admin": "password123",
//	    })),
//	)
func WithAuth(mw gin.HandlerFunc) Option {
	return func(o *options) {
		o.authMw = mw
	}
}

// Register 将 pprof 路由注册到 Gin 引擎路由组中。
//
// 注册的路由（以默认前缀 /debug/pprof 为例）：
//   - /debug/pprof/            - pprof 首页
//   - /debug/pprof/cmdline     - 命令行参数
//   - /debug/pprof/profile     - CPU profile（30 秒采样）
//   - /debug/pprof/symbol      - 符号查询（GET/POST）
//   - /debug/pprof/trace       - 执行轨迹
//   - /debug/pprof/allocs      - 内存分配
//   - /debug/pprof/block       - 阻塞分析
//   - /debug/pprof/goroutine   - 协程堆栈
//   - /debug/pprof/heap        - 堆内存
//   - /debug/pprof/mutex       - 互斥锁
//   - /debug/pprof/threadcreate - 线程创建
//   - /debug/pprof/profile-io  - IO 等待时间（需 WithIOWaitTime）
//
// 如果设置了 WithAuth，所有 pprof 路由都经过鉴权中间件检查。
func Register(r *gin.Engine, opts ...Option) {
	o := &options{prefix: DefaultPrefix}
	o.apply(opts...)

	group := r.Group(o.prefix)

	// 可选鉴权中间件
	if o.authMw != nil {
		group.Use(o.authMw)
	}

	group.GET("/", gin.WrapF(pprof.Index))
	group.GET("/cmdline", gin.WrapF(pprof.Cmdline))
	group.GET("/profile", gin.WrapF(pprof.Profile))
	group.POST("/symbol", gin.WrapF(pprof.Symbol))
	group.GET("/symbol", gin.WrapF(pprof.Symbol))
	group.GET("/trace", gin.WrapF(pprof.Trace))
	group.GET("/allocs", gin.WrapH(pprof.Handler("allocs")))
	group.GET("/block", gin.WrapH(pprof.Handler("block")))
	group.GET("/goroutine", gin.WrapH(pprof.Handler("goroutine")))
	group.GET("/heap", gin.WrapH(pprof.Handler("heap")))
	group.GET("/mutex", gin.WrapH(pprof.Handler("mutex")))
	group.GET("/threadcreate", gin.WrapH(pprof.Handler("threadcreate")))

	if o.enableIOWaitTime {
		// Similar to /profile, add IO wait time, https://github.com/felixge/fgprof
		group.GET("/profile-io", gin.WrapH(fgprof.Handler()))
	}
}
