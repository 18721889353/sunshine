package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/18721889353/sunshine/pkg/errcode"
)

// smokeResponse 用 httptest 跑一次响应，返回状态码与解析后的 JSON body。
func smokeResponse(t *testing.T, handler gin.HandlerFunc) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	handler(c)

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (body=%s)", err, w.Body.String())
	}
	return w.Code, body
}

// TestSmoke_Success 成功响应：HTTP 200 + data 透传。
func TestSmoke_Success(t *testing.T) {
	status, body := smokeResponse(t, func(c *gin.Context) {
		Success(c, "hello")
	})
	if status != http.StatusOK {
		t.Errorf("status 应为 200, got %d", status)
	}
	if body["data"] != "hello" {
		t.Errorf("data 应透传 hello, got %v", body["data"])
	}
}

// TestSmoke_Error 业务错误响应：HTTP 200 平铺 + body code 为业务码。
func TestSmoke_Error(t *testing.T) {
	e := errcode.NewError(40001, "test error")
	status, body := smokeResponse(t, func(c *gin.Context) {
		Error(c, e)
	})
	if status != http.StatusOK {
		t.Errorf("业务错误应 HTTP 200 平铺, got %d", status)
	}
	if body["code"] == nil {
		t.Errorf("body 应含 code 字段, got %v", body)
	}
}

// TestSmoke_Output 标准状态码响应：HTTP 400 + body code 为 400。
func TestSmoke_Output(t *testing.T) {
	status, body := smokeResponse(t, func(c *gin.Context) {
		Output(c, http.StatusBadRequest)
	})
	if status != http.StatusBadRequest {
		t.Errorf("status 应为 400, got %d", status)
	}
	if body["code"] == nil {
		t.Errorf("body 应含 code 字段, got %v", body)
	}
}

// TestSmoke_Out 错误码到 HTTP 状态码映射响应。
func TestSmoke_Out(t *testing.T) {
	e := errcode.NewError(50001, "server error")
	status, body := smokeResponse(t, func(c *gin.Context) {
		Out(c, e)
	})
	if status < 400 {
		t.Errorf("Out 错误应返回 >=400, got %d", status)
	}
	if body["code"] == nil {
		t.Errorf("body 应含 code 字段, got %v", body)
	}
}
