package prof

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/18721889353/sunshine/pkg/utils"
)

func TestRegister(t *testing.T) {
	r := gin.Default()
	Register(r, WithPrefix(""), WithPrefix("/myServer"), WithIOWaitTime())

	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()
	httpServer := &http.Server{
		Addr:    serverAddr,
		Handler: r,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic("listen and serve error: " + err.Error())
		}
	}()
	time.Sleep(time.Millisecond * 200)
	defer httpServer.Close()

	resp, err := http.Get(requestAddr + "/myServer/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

func TestRegister_WithAuth(t *testing.T) {
	r := gin.Default()
	Register(r, WithPrefix("/pprof-auth"),
		WithAuth(gin.BasicAuth(gin.Accounts{"admin": "pass"})),
	)

	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()
	httpServer := &http.Server{
		Addr:    serverAddr,
		Handler: r,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic("listen and serve error: " + err.Error())
		}
	}()
	time.Sleep(time.Millisecond * 200)
	defer httpServer.Close()

	// 无认证应返回 401
	resp, err := http.Get(requestAddr + "/pprof-auth/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp.Body.Close()

	// 正确认证应返回 200
	req, _ := http.NewRequest("GET", requestAddr+"/pprof-auth/", nil)
	req.SetBasicAuth("admin", "pass")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}
