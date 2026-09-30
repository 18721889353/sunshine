package prof

import (
	"net/http"
	"net/http/pprof"
	"sync/atomic"

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

// httpPprofEnabled 控制 HTTP pprof 路由是否生效，支持热更新
var httpPprofEnabled atomic.Bool

// httpPprofAuthFn 存储当前鉴权中间件，支持运行时热替换；nil 表示不启用鉴权。
var httpPprofAuthFn atomic.Pointer[func(http.Handler) http.Handler]

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
// 注册后仍可通过 SetPprofAuth 在运行时热替换鉴权逻辑。
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

// SetPprofEnabled 动态设置 pprof 路由是否生效，支持 Nacos 热更新。
// enabled=true 时允许访问，enabled=false 时拒绝访问。
func SetPprofEnabled(enabled bool) {
	httpPprofEnabled.Store(enabled)
}

// SetPprofAuth 动态设置 pprof 鉴权中间件，支持 Nacos 热更新。
// fn=nil 时关闭鉴权，非 nil 时替换为新的鉴权逻辑，下次请求即生效。
//
// 示例:
//
//	prof.SetPprofAuth(pprofIPWhitelist(config.Get().App.PprofIPWhiteList))
func SetPprofAuth(fn func(http.Handler) http.Handler) {
	if fn == nil {
		httpPprofAuthFn.Store(nil)
		return
	}
	httpPprofAuthFn.Store(&fn)
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
// 鉴权中间件支持通过 SetPprofAuth 在运行时热替换（如白名单配置变更后即时生效）。
func Register(mux *http.ServeMux, opts ...HTTPOption) {
	o := &httpOptions{prefix: DefaultPrefix}
	o.apply(opts...)

	// 设置初始开关状态
	httpPprofEnabled.Store(true)

	// 用本次注册的配置初始化鉴权中间件（nil 也写入，确保未配置鉴权时不受历史值影响），
	// 后续可通过 SetPprofAuth 热替换
	SetPprofAuth(o.authFn)

	// 动态开关中间件：根据 httpPprofEnabled 原子变量控制是否放行
	enabledCheck := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !httpPprofEnabled.Load() {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// 标准 pprof 路由
	mux.Handle(o.prefix+"/", wrapHandler(enabledCheck, http.HandlerFunc(pprof.Index)))
	mux.Handle(o.prefix+"/profile", wrapHandler(enabledCheck, http.HandlerFunc(pprof.Profile)))
	mux.Handle(o.prefix+"/symbol", wrapHandler(enabledCheck, http.HandlerFunc(pprof.Symbol)))
	mux.Handle(o.prefix+"/cmdline", wrapHandler(enabledCheck, http.HandlerFunc(pprof.Cmdline)))
	mux.Handle(o.prefix+"/trace", wrapHandler(enabledCheck, http.HandlerFunc(pprof.Trace)))

	// 按名称查询的 profile 类型
	mux.Handle(o.prefix+"/allocs", wrapHandler(enabledCheck, pprof.Handler("allocs")))
	mux.Handle(o.prefix+"/heap", wrapHandler(enabledCheck, pprof.Handler("heap")))
	mux.Handle(o.prefix+"/goroutine", wrapHandler(enabledCheck, pprof.Handler("goroutine")))
	mux.Handle(o.prefix+"/threadcreate", wrapHandler(enabledCheck, pprof.Handler("threadcreate")))
	mux.Handle(o.prefix+"/block", wrapHandler(enabledCheck, pprof.Handler("block")))
	mux.Handle(o.prefix+"/mutex", wrapHandler(enabledCheck, pprof.Handler("mutex")))

	// IO 等待时间 profile（类似 /profile，额外包含 IO 等待时间）
	if o.enableIOWaitTime {
		mux.Handle(o.prefix+"/profile-io", wrapHandler(enabledCheck, fgprof.Handler()))
	}
}

// wrapHandler 用鉴权和开关中间件包装最终的 handler。
// enabledCheck: 开关检查中间件
// handler: 最终的 pprof handler
//
// 鉴权中间件从 httpPprofAuthFn 原子变量动态加载，每次请求读取最新值，
// 支持通过 SetPprofAuth 在运行时热替换鉴权逻辑。
func wrapHandler(enabledCheck func(http.Handler) http.Handler, handler http.Handler) http.Handler {
	// 先包装开关检查
	handler = enabledCheck(handler)
	// 再包装鉴权：每次请求从原子变量加载最新鉴权中间件，支持热更新
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fnPtr := httpPprofAuthFn.Load(); fnPtr != nil {
			(*fnPtr)(handler).ServeHTTP(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})
}
