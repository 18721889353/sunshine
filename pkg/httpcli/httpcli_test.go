package httpcli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/tidwall/gjson"
	"log"
	"net/http"
	"testing"
	"time"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestHTTPClient(t *testing.T) {

	customTransport := &http.Transport{
		MaxIdleConns:          100,              // 总的最大空闲连接数
		MaxIdleConnsPerHost:   0,                // 每个主机的最大空闲连接数
		IdleConnTimeout:       90 * time.Second, // 空闲连接超时时间
		TLSHandshakeTimeout:   10 * time.Second, // TLS 握手超时时间
		ExpectContinueTimeout: 1 * time.Second,  // Expect: 100-continue 超时时间
	}

	// 创建普通客户端
	client := New(
		WithBaseURL("http://127.0.0.1"),
		WithTimeout(5*time.Second),
		WithTransport(customTransport),
	)

	//// GET请求示例
	//var getUser User
	//resp, err := client.Request(context.Background()).
	//	SetResult(&getUser).
	//	Get("/api/v1/auth/login")
	//if err != nil {
	//	t.Fatalf("GET request failed: %v", err)
	//}
	//
	//if resp.IsSuccess() {
	//	fmt.Printf("User: %+v\n", getUser)
	//}

	// POST请求示例
	newUser := map[string]interface{}{
		"username":    "admin",
		"password":    "123456",
		"captchaKey":  "fXJ1mBnUG3dc87SBhhUU",
		"captchaCode": "debug",
		"timestamp":   "1712626580",
		"nonce_str":   "a3998809da7aed4470d814cdb065314b",
		"sign":        "debug",
	}
	//【有道云笔记】gjson
	//https://note.youdao.com/s/A9otNX4c
	resp, err := client.Request(context.Background()).
		SetBody(newUser).
		Post("/api/v1/auth/login")
	code := gjson.Get(resp.String(), "data.accessToken")
	fmt.Println(resp, code, err)
	//fmt.Printf("resp: %+v err:%v\n", resp, err)

	//if resp.IsSuccess() {
	//	fmt.Printf("Created user: %+v\n", createdUser)
	//}
}

func TestHTTPSClientWithTLS(t *testing.T) {
	// 创建带TLS验证的客户端
	client := New(
		WithBaseURL("https://api.example.com"),
		WithRootCA("path/to/ca.crt"),
		WithClientCert("path/to/client.crt", "path/to/client.key"),
		WithTimeout(10*time.Second),
	)

	// 发送安全请求
	resp, err := client.Request(context.Background()).
		SetHeader("X-Request-ID", "12345").
		Get("/secure-endpoint")
	if err != nil {
		t.Fatalf("Secure request failed: %v", err)
	}

	fmt.Printf("Response status: %d\n", resp.StatusCode())
}

func TestErrorHandling(t *testing.T) {
	client := New(
		WithBaseURL("https://jsonplaceholder.typicode.com"),
	)

	// 请求不存在的资源
	_, err := client.Request(context.Background()).
		Get("/nonexistent")
	if err != nil {
		var httpErr *ErrorResponse
		if errors.As(err, &httpErr) {
			log.Printf("HTTP error: %d - %s", httpErr.StatusCode, httpErr.Message)
			log.Printf("Response body: %s", string(httpErr.Body))
		} else {
			t.Fatalf("Unexpected error type: %v", err)
		}
	}
}

func TestCustomTLSConfig(t *testing.T) {
	// 自定义TLS配置
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
	}

	_ = New(
		WithBaseURL("https://api.example.com"),
		WithTLSConfig(tlsConfig),
	)

	// 发送请求...
}
