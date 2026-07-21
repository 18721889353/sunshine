package prof

import (
	"net/http"
	"net/http/pprof"

	"github.com/felixge/fgprof"
)

const (
	// DefaultPrefix 默认 pprof 路由前缀
	DefaultPrefix = "/debug/pprof"
)

// HTTPOption 定义 HTTP pprof 注册的配置选项函数
type HTTPOption func(o *httpOptions)

type httpOptions struct {
	prefix           string
	enableIOWaitTime bool
	authFn           func(http.Handler) http.Handler // 鉴权中间件，nil 表示不启用
}

func (o *httpOptions) apply(opts ...HTTPOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithPrefix 设置 pprof 路由前缀。
// 如果 prefix 为空字符串，则使用默认前缀 /debug/pprof。
func WithPrefix(prefix string) HTTPOption {
	return func(o *httpOptions) {
		if prefix != "" {
			o.prefix = prefix
		}
	}
}

// WithIOWaitTime 启用 IO 等待时间 profile 分析。
// 开启后在 {prefix}/profile-io 路由提供 fgprof 分析，
// 它比标准 pprof 多包含 IO 等待时间，更适合分析 IO 密集型服务。
func WithIOWaitTime() HTTPOption {
	return func(o *httpOptions) {
		o.enableIOWaitTime = true
	}
}

// WithAuth 设置 pprof 路由的鉴权中间件。
// 在生成环境中，pprof 可能暴露敏感信息（如源码路径、goroutine 堆栈等），
// 建议通过此选项添加鉴权保护。
//
// 示例:
//
//	// 简单 Token 鉴权
//	auth := func(next http.Handler) http.Handler {
//	    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//	        if r.Header.Get("X-Auth-Token") != "my-secret-token" {
//	            http.Error(w, "Forbidden", http.StatusForbidden)
//	            return
//	        }
//	        next.ServeHTTP(w, r)
//	    })
//	}
//	prof.Register(mux, prof.WithAuth(auth))
func WithAuth(authFn func(http.Handler) http.Handler) HTTPOption {
	return func(o *httpOptions) {
		o.authFn = authFn
	}
}

// Register 将 pprof 路由注册到标准 http.ServeMux 中。
//
// 注册的路由（以默认前缀 /debug/pprof 为例）：
//   - /debug/pprof/            - pprof 首页
//   - /debug/pprof/cmdline     - 命令行参数
//   - /debug/pprof/profile     - CPU profile（30 秒采样）
//   - /debug/pprof/symbol      - 符号查询
//   - /debug/pprof/trace       - 执行轨迹
//   - /debug/pprof/allocs      - 内存分配
//   - /debug/pprof/block       - 阻塞分析
//   - /debug/pprof/goroutine   - 协程堆栈
//   - /debug/pprof/heap        - 堆内存
//   - /debug/pprof/mutex       - 互斥锁
//   - /debug/pprof/threadcreate - 线程创建
//   - /debug/pprof/profile-io  - IO 等待时间（需 WithIOWaitTime）
//
// 如果设置了 WithAuth，所有 pprof 路由都将经过鉴权中间件检查。
func Register(mux *http.ServeMux, opts ...HTTPOption) {
	o := &httpOptions{prefix: DefaultPrefix}
	o.apply(opts...)

	// 标准 pprof 路由
	mux.Handle(o.prefix+"/", wrapHandler(o.authFn, http.HandlerFunc(pprof.Index)))
	mux.Handle(o.prefix+"/profile", wrapHandler(o.authFn, http.HandlerFunc(pprof.Profile)))
	mux.Handle(o.prefix+"/symbol", wrapHandler(o.authFn, http.HandlerFunc(pprof.Symbol)))
	mux.Handle(o.prefix+"/cmdline", wrapHandler(o.authFn, http.HandlerFunc(pprof.Cmdline)))
	mux.Handle(o.prefix+"/trace", wrapHandler(o.authFn, http.HandlerFunc(pprof.Trace)))

	// 按名称查询的 profile 类型
	mux.Handle(o.prefix+"/allocs", wrapHandler(o.authFn, pprof.Handler("allocs")))
	mux.Handle(o.prefix+"/heap", wrapHandler(o.authFn, pprof.Handler("heap")))
	mux.Handle(o.prefix+"/goroutine", wrapHandler(o.authFn, pprof.Handler("goroutine")))
	mux.Handle(o.prefix+"/threadcreate", wrapHandler(o.authFn, pprof.Handler("threadcreate")))
	mux.Handle(o.prefix+"/block", wrapHandler(o.authFn, pprof.Handler("block")))
	mux.Handle(o.prefix+"/mutex", wrapHandler(o.authFn, pprof.Handler("mutex")))

	// IO 等待时间 profile（类似 /profile，额外包含 IO 等待时间）
	if o.enableIOWaitTime {
		mux.Handle(o.prefix+"/profile-io", wrapHandler(o.authFn, fgprof.Handler()))
	}
}

// wrapHandler 如果 authFn 不为 nil，则用鉴权中间件包装 handler
func wrapHandler(authFn func(http.Handler) http.Handler, handler http.Handler) http.Handler {
	if authFn != nil {
		return authFn(handler)
	}
	return handler
}
