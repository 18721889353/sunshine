package metrics

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/gin/handlerfunc"
	"github.com/18721889353/sunshine/pkg/utils"
)

func TestMetrics(t *testing.T) {
	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(Metrics(r,
		WithMetricsPath("/metrics"),
		WithIgnoreStatusCodes(http.StatusNotFound),
		WithIgnoreRequestPaths("/hello-ignore"),
		WithIgnoreRequestMethods(http.MethodDelete),
	))
	r.GET("ping", handlerfunc.Ping)
	r.GET("/hello", func(c *gin.Context) {
		c.String(200, "[get] hello")
	})

	go func() {
		err := r.Run(serverAddr)
		if err != nil {
			panic(err)
		}
	}()

	// 轮询等服务器就绪（替代固定 200ms sleep）：慢 CI 上服务器未启动
	// 会让下面的 http.Get connection refused（flaky）
	ready := false
	for i := 0; i < 100 && !ready; i++ {
		if resp, err := http.Get(requestAddr + "/ping"); err == nil {
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("gin 服务器未在 2s 内就绪")
	}

	resp, err := http.Get(requestAddr + "/ping")
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	resp, err = http.Get(requestAddr + "/hello")
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	resp, err = http.Get(requestAddr + "/metrics")
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}
