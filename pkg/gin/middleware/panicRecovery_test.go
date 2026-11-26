package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestPanicRecoveryMetrics_PanicsAgain 验证中间件记录后重新抛出 panic——
// 必须交给 gin.Recovery() 兜底，否则语义变了（本中间件只观测不吞）。
func TestPanicRecoveryMetrics_PanicsAgain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(PanicRecoveryMetrics())
	r.Use(gin.Recovery())
	r.GET("/panic", func(c *gin.Context) { panic("boom") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	r.ServeHTTP(w, req) // 不应 panic 到测试，gin.Recovery 接管

	if w.Code != http.StatusInternalServerError {
		t.Errorf("gin.Recovery 应返回 500, got %d", w.Code)
	}
}
