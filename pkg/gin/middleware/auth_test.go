package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/18721889353/sunshine/pkg/jwt"
)

// 本文件覆盖 WithJwtManager 实例注入的行为：
// 注入后合法 token 放行、未注入时 401 拒绝（fail loud，不 panic）。

// newAuthTestManager 创建 auth 测试专用的 jwt.Manager（固定测试密钥）。
func newAuthTestManager(t testing.TB) *jwt.Manager {
	t.Helper()
	mgr, err := jwt.New(jwt.WithSigningKey("TEST_ONLY_middleware_test_signing_key"))
	require.NoError(t, err, "创建测试 Manager 失败")
	return mgr
}

// TestAuthWithJwtManagerAllowsValidToken 验证注入 Manager 后合法 token 放行，
// 且 uid 被正确提取到 gin.Context 中。
func TestAuthWithJwtManagerAllowsValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr := newAuthTestManager(t)
	token, err := mgr.GenerateToken("10001", "sunshine", map[string]any{"role": "admin"})
	require.NoError(t, err)

	authorization := "Bearer " + token
	require.GreaterOrEqual(t, len(authorization), 150, "测试 token 需满足中间件最短头长度校验")

	r := gin.New()
	r.Use(Auth(WithSwitchHTTPCode(), WithJwtManager(mgr)))
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok:"+c.GetString("uid"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(HeaderAuthorizationKey, authorization)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok:10001", w.Body.String())
}

// TestAuthWithoutJwtManagerRejects 验证未注入 Manager（nil）时请求被 401 拒绝，
// 沿用 responseUnauthorized 路径，不 panic。
func TestAuthWithoutJwtManagerRejects(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(Auth(WithSwitchHTTPCode()))
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	// 构造长度足够但无法解析的请求头：nil 检查先于头校验，确保测的是未注入路径
	req.Header.Set(HeaderAuthorizationKey, "Bearer "+strings.Repeat("x", 200))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
