package gows

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// TestUpgrade_CORSDefaultRejected: 默认 CORS 拒绝
// ---------------------------------------------------------------------------

// TestUpgrade_CORSDefaultRejected: 默认 CORS 拒绝所有来源
// CORS 检查在调用 gorilla.Upgrade 之前执行，httptest 即可验证
func TestUpgrade_CORSDefaultRejected(t *testing.T) {
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		_, err := Upgrade(c)
		if err == nil {
			t.Error("Upgrade should fail with default CORS (reject all)")
			return
		}
		if !strings.Contains(err.Error(), "CORS check rejected") {
			t.Errorf("unexpected error: %v", err)
		}
		c.String(http.StatusForbidden, err.Error())
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "http://evil.com")
	r.ServeHTTP(w, req)

	t.Log("默认 CORS 拒绝正确")
}

// TestUpgrade_CORSWithCheckOrigin: 显式 CORS 允许
// 使用真实的 WebSocket 拨号验证升级成功
func TestUpgrade_CORSWithCheckOrigin(t *testing.T) {
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		_, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return true }),
		)
		if err != nil {
			t.Errorf("Upgrade should succeed: %v", err)
		}
	})

	s := httptest.NewServer(r)
	defer s.Close()

	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Origin": {"http://trusted.com"},
	})
	if err != nil {
		t.Fatalf("WebSocket Dial 应成功: %v", err)
	}
	_ = conn.Close()
	t.Log("显式 CORS 升级成功，WebSocket 连接已建立")
}

// TestUpgrade_CORSRejectedByFunc: CORS 函数返回 false
func TestUpgrade_CORSRejectedByFunc(t *testing.T) {
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		_, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return false }),
		)
		if err == nil {
			t.Error("Upgrade should fail when CheckOrigin returns false")
			return
		}
		if !strings.Contains(err.Error(), "CORS check rejected") {
			t.Errorf("unexpected error: %v", err)
		}
		c.String(http.StatusForbidden, err.Error())
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "http://evil.com")
	r.ServeHTTP(w, req)

	t.Log("CORS 函数拒绝正确")
}

// TestUpgrade_RateLimit: 全局速率限制
// 注意: WithRateLimit 设置的是全局 rate.Limiter，测试须提前设置好全局状态
func TestUpgrade_RateLimit(t *testing.T) {
	// 先清理可能残留的限流器
	upgradeLimiter.Store(rate.NewLimiter(1, 1)) // 1 rps, burst=1
	defer upgradeLimiter.Store(nil)

	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		// 使用匿名选项仅设置 enableRateLimit 标志，不重建限流器
		// 限流器已在测试层面预创建，避免 WithRateLimit 每次创建新实例
		_, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return true }),
			func(o *upgradeOptions) { o.enableRateLimit = true },
		)
		if err != nil {
			c.String(http.StatusTooManyRequests, err.Error())
			return
		}
	})

	s := httptest.NewServer(r)
	defer s.Close()

	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"

	var successCount, failCount int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{
				"Origin": {"http://test.com"},
			})
			if err == nil {
				atomic.AddInt32(&successCount, 1)
				conn.Close()
			} else {
				atomic.AddInt32(&failCount, 1)
			}
		}()
	}
	wg.Wait()

	t.Logf("速率限制: success=%d fail=%d (burst=1, 仅 1 个应成功)", successCount, failCount)
	if failCount == 0 {
		t.Error("预期部分请求应被限流")
	}
}

// TestUpgrade_MaxConnPerIP: 单 IP 连接数限制
// 使用真实的 WebSocket 连接，通过 blockCh 保持连接打开
func TestUpgrade_MaxConnPerIP(t *testing.T) {
	blockCh := make(chan struct{})
	defer close(blockCh)

	// 清理残留的 per-IP 计数
	perIPConns.Range(func(k, v interface{}) bool {
		perIPConns.Delete(k)
		return true
	})

	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		cc, err := Upgrade(c,
			WithCheckOrigin(func(r *http.Request) bool { return true }),
			WithMaxConnPerIP(2),
		)
		if err != nil {
			c.String(http.StatusTooManyRequests, err.Error())
			return
		}
		_ = cc
		// 保持连接打开，让 IP 计数累积
		<-blockCh
	})

	s := httptest.NewServer(r)
	defer s.Close()

	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"

	// 前 2 个连接应成功
	var conns []*websocket.Conn
	for i := 0; i < 2; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{
			"Origin": {"http://test.com"},
		})
		if err != nil {
			t.Fatalf("连接 %d 应成功: %v", i, err)
		}
		conns = append(conns, conn)
	}
	t.Logf("前 2 个连接建立成功")

	// 第 3 个连接应被拒绝
	_, resp, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Origin": {"http://test.com"},
	})
	if err != nil {
		t.Logf("第 3 个连接被正确拒绝: %v", err)
		if resp != nil {
			t.Logf("HTTP 状态码: %d", resp.StatusCode)
		}
	} else {
		t.Error("第 3 个连接应被单 IP 限制拒绝")
	}

	for _, c := range conns {
		c.Close()
	}
}
