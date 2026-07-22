package middleware

import (
	"net/http"
	"testing"
	"time"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/utils"
)

func TestParseTokenCalculateStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected flow.TokenCalculateStrategy
	}{
		{"direct", flow.Direct},
		{"warmUp", flow.WarmUp},
		{"unknown", flow.Direct},
		{"", flow.Direct},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseTokenCalculateStrategy(tt.input))
		})
	}
}

func TestParseControlBehavior(t *testing.T) {
	tests := []struct {
		input    string
		expected flow.ControlBehavior
	}{
		{"reject", flow.Reject},
		{"throttling", flow.Throttling},
		{"unknown", flow.Reject},
		{"", flow.Reject},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseControlBehavior(tt.input))
		})
	}
}

func TestParseBreakerStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected circuitbreaker.Strategy
	}{
		{"errorRatio", circuitbreaker.ErrorRatio},
		{"errorCount", circuitbreaker.ErrorCount},
		{"slowRequestRatio", circuitbreaker.SlowRequestRatio},
		{"unknown", circuitbreaker.ErrorRatio},
		{"", circuitbreaker.ErrorRatio},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseBreakerStrategy(tt.input))
		})
	}
}

func TestSentinelMiddleware(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	mw := SentinelMiddleware(
		WithSentinelResourceExtractor(func(c *gin.Context) string {
			return c.FullPath()
		}),
		WithSentinelFlowRules([]*flow.Rule{
			{
				Resource:               "/test",
				TokenCalculateStrategy: flow.Direct,
				ControlBehavior:        flow.Reject,
				Threshold:              1000,
				StatIntervalInMs:       1000,
			},
		}),
	)
	assert.NotNil(t, mw)

	c, _ := gin.CreateTestContext(nil)
	c.Request, _ = http.NewRequest(http.MethodGet, "/test", nil)
	mw(c)
}

func TestCircuitBreakerMiddleware(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	degradeCalled := false
	degradeHandler := func(c *gin.Context) {
		degradeCalled = true
		response.Output(c, http.StatusOK, "degrade")
	}

	mw := CircuitBreaker(WithDegradeHandler(degradeHandler))
	assert.NotNil(t, mw)

	c, _ := gin.CreateTestContext(nil)
	c.Request, _ = http.NewRequest(http.MethodGet, "/test", nil)
	mw(c)
	assert.False(t, degradeCalled)
}

func TestCircuitBreakerHTTPServer(t *testing.T) {
	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()

	degradeHandler := func(c *gin.Context) {
		response.Output(c, http.StatusOK, "degrade")
	}

	initSentinel(nil, []*circuitbreaker.Rule{
		{
			Resource:         "/hello",
			Strategy:         circuitbreaker.ErrorRatio,
			RetryTimeoutMs:   10000,
			MinRequestAmount: 3,
			StatIntervalMs:   10000,
			Threshold:        0.3,
		},
	})

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(CircuitBreaker(WithDegradeHandler(degradeHandler)))

	r.GET("/hello", func(c *gin.Context) {
		response.Success(c, "localhost"+serverAddr)
	})

	go func() {
		err := r.Run(serverAddr)
		if err != nil {
			panic(err)
		}
	}()

	time.Sleep(time.Millisecond * 200)

	resp, err := http.Get("http://" + requestAddr + "/hello")
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	if resp != nil {
		resp.Body.Close()
	}
}

func TestErrNotAllowed(t *testing.T) {
	assert.Equal(t, "circuitbreaker: not allowed for circuit open", ErrNotAllowed.Error())
}

func TestSentinelRateLimit_Allow(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	// 高阈值限流，请求应正常通过
	mw := SentinelMiddleware(
		WithSentinelResourceExtractor(func(c *gin.Context) string {
			return c.FullPath()
		}),
		WithSentinelFlowRules([]*flow.Rule{
			{
				Resource:               "/allow",
				TokenCalculateStrategy: flow.Direct,
				ControlBehavior:        flow.Reject,
				Threshold:              10000,
				StatIntervalInMs:       1000,
			},
		}),
	)
	assert.NotNil(t, mw)

	c, _ := gin.CreateTestContext(nil)
	c.Request, _ = http.NewRequest(http.MethodGet, "/allow", nil)
	mw(c)
	assert.Equal(t, http.StatusOK, c.Writer.Status())
}

func TestSentinelMiddleware_WithBreakerRules(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	mw := SentinelMiddleware(
		WithSentinelResourceExtractor(func(c *gin.Context) string {
			return c.FullPath()
		}),
		WithSentinelCircuitBreakerRules([]*circuitbreaker.Rule{
			{
				Resource:         "/breaker-test",
				Strategy:         circuitbreaker.ErrorRatio,
				RetryTimeoutMs:   10000,
				MinRequestAmount: 3,
				StatIntervalMs:   10000,
				Threshold:        0.5,
			},
		}),
	)
	assert.NotNil(t, mw)

	c, _ := gin.CreateTestContext(nil)
	c.Request, _ = http.NewRequest(http.MethodGet, "/breaker-test", nil)
	mw(c)
}

func TestSentinelRateLimitHTTPServer(t *testing.T) {
	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()

	// 限流规则：1 QPS，超出则拒绝
	initSentinel([]*flow.Rule{
		{
			Resource:               "/limit",
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject,
			Threshold:              1,
			StatIntervalInMs:       1000,
		},
	}, nil)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(SentinelMiddleware(
		WithSentinelResourceExtractor(func(c *gin.Context) string {
			return c.FullPath()
		}),
	))

	r.GET("/limit", func(c *gin.Context) {
		response.Success(c, "ok")
	})

	go func() {
		err := r.Run(serverAddr)
		if err != nil {
			panic(err)
		}
	}()

	time.Sleep(time.Millisecond * 500)

	// 第一次请求应成功
	resp1, err1 := http.Get("http://" + requestAddr + "/limit")
	assert.NoError(t, err1)
	if resp1 != nil {
		resp1.Body.Close()
	}

	// 第二次快速请求可能被限流（1 QPS），但不应该 panic
	resp2, err2 := http.Get("http://" + requestAddr + "/limit")
	assert.NoError(t, err2)
	if resp2 != nil {
		resp2.Body.Close()
	}
}
