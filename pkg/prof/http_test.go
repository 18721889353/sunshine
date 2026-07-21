package prof

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/18721889353/sunshine/pkg/utils"
)

func TestRegister(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, WithPrefix(""), WithPrefix("/myServer"), WithIOWaitTime())

	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()
	httpServer := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic("listen and serve error: " + err.Error())
		}
	}()
	time.Sleep(time.Millisecond * 200)
	defer httpServer.Close()

	// Test pprof index
	resp, err := http.Get(requestAddr + "/myServer/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Test individual pprof endpoints
	endpoints := []string{
		"/myServer/cmdline",
		"/myServer/goroutine",
		"/myServer/heap",
		"/myServer/allocs",
	}

	for _, ep := range endpoints {
		resp, err := http.Get(requestAddr + ep)
		require.NoError(t, err, "endpoint: %s", ep)
		assert.Equal(t, http.StatusOK, resp.StatusCode, "endpoint: %s", ep)
		resp.Body.Close()
	}
}

func TestRegister_WithAuth(t *testing.T) {
	mux := http.NewServeMux()

	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Auth-Token") != "secret" {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	Register(mux, WithPrefix("/pprof-auth"), WithAuth(auth))

	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()
	httpServer := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic("listen and serve error: " + err.Error())
		}
	}()
	time.Sleep(time.Millisecond * 200)
	defer httpServer.Close()

	// 无 Token 应返回 403
	resp, err := http.Get(requestAddr + "/pprof-auth/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp.Body.Close()

	// 正确 Token 应返回 200
	req, _ := http.NewRequest("GET", requestAddr+"/pprof-auth/", nil)
	req.Header.Set("X-Auth-Token", "secret")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// 错误 Token 应返回 403
	req, _ = http.NewRequest("GET", requestAddr+"/pprof-auth/heap", nil)
	req.Header.Set("X-Auth-Token", "wrong")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp.Body.Close()
}

func TestRegister_WithIOWaitTime(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, WithPrefix("/io-pprof"), WithIOWaitTime())

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()

	time.Sleep(time.Millisecond * 200)
	addr := listener.Addr().String()

	// 验证 pprof 首页正常返回即可（/profile-io 会阻塞，不测试）
	resp, err := http.Get("http://" + addr + "/io-pprof/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

func TestRegister_DefaultPrefix(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)

	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()
	httpServer := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic("listen and serve error: " + err.Error())
		}
	}()
	time.Sleep(time.Millisecond * 200)
	defer httpServer.Close()

	resp, err := http.Get(requestAddr + "/debug/pprof/heap")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}
