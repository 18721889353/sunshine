package middleware

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// requestTimeout 全局请求超时时间（单位纳秒），支持热更新。
// 值小于 1 毫秒时表示不限制请求超时。
var requestTimeout atomic.Int64

// SetRequestTimeout 动态设置全局请求超时时间（支持 Nacos 热更新）。
// 传入 0 或小于 1 毫秒表示不限制请求超时。
// 注意：该设置对所有由 Timeout 创建的中间件实例生效。
func SetRequestTimeout(d time.Duration) {
	requestTimeout.Store(int64(d))
}

// Timeout 返回一个 Gin 中间件，为每个 HTTP 请求设置超时控制。
//
// 当请求处理时间超过指定的超时时间时，中间件会中断请求并返回
// 504 Gateway Timeout 状态码。如果请求在超时前已完成但状态码尚未
// 写入（仍为 200），同样会覆盖为 504。
//
// 超时时间支持热更新：中间件每次请求时从全局配置 requestTimeout 读取最新值，
// 创建时传入的 d 作为初始值，后续可通过 SetRequestTimeout 动态修改。
//
// 参数:
//   - d: 超时时间（初始值）。小于 1 毫秒表示初始不启用超时。
//
// 返回值:
//   - gin.HandlerFunc: Gin 中间件处理函数。
func Timeout(d time.Duration) gin.HandlerFunc {
	// 记录初始超时时间（多次调用时以最后一次为准）
	requestTimeout.Store(int64(d))

	return func(c *gin.Context) {
		// 每次请求读取最新超时时间（支持热更新）
		cur := time.Duration(requestTimeout.Load())
		// 边界条件：超时时间过短时不启用超时控制。
		if cur < time.Millisecond {
			c.Next()
			return
		}

		// 使用父级 context 创建带超时的派生 context，确保请求处理不超过指定时间。
		ctx, cancel := context.WithTimeout(c.Request.Context(), cur)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		// 超时判断：当 context 因超时而取消时，若状态码尚未写入（仍为 200）
		// 则返回 504，否则直接终止请求链。
		if ctx.Err() == context.DeadlineExceeded {
			if c.Writer.Status() == 200 {
				c.AbortWithStatus(http.StatusGatewayTimeout)
				return
			}
			c.Abort()
		}
	}
}
