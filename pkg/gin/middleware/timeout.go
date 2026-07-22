package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout 返回一个 Gin 中间件，为每个 HTTP 请求设置超时控制。
//
// 当请求处理时间超过指定的超时时间时，中间件会中断请求并返回
// 504 Gateway Timeout 状态码。如果请求在超时前已完成但状态码尚未
// 写入（即 c.Writer.Status() == 200），同样会覆盖为 504。
//
// 参数:
//   - d: 超时时间。如果小于 1 毫秒，返回一个空操中间件（不生效）。
//
// 返回值:
//   - gin.HandlerFunc: Gin 中间件处理函数。
func Timeout(d time.Duration) gin.HandlerFunc {
	// 边界条件：超时时间过短时返回空操作中间件，避免无效的超时控制。
	if d < time.Millisecond {
		return func(_ *gin.Context) {}
	}

	return func(c *gin.Context) {
		// 使用父级 context 创建带超时的派生 context，确保请求处理不超过指定时间。
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
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
