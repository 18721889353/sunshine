package middleware

import (
	"github.com/gin-gonic/gin"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// PanicRecoveryMetrics 记录 gin 请求链路中的 panic 并重新抛出，
// 由后续 gin.Recovery() 负责兜底返回 500。
//
// 必须注册在 gin.Recovery() 【之前】——否则 panic 被 Recovery 吞掉，
// 本中间件永远观察不到（顺序错了指标恒为 0，是典型静默失效）。
//
// panic 是最严重的系统故障：进程没挂但请求失败，只能靠日志发现——
// 本指标补上"metrics 可见"这一环。
func PanicRecoveryMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				pkgmetrics.ObservePanic("gin-http")
				panic(r) // 交还 gin.Recovery() 处理响应
			}
		}()
		c.Next()
	}
}
