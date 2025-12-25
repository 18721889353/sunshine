package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestConcurrentLoggingMiddleware(t *testing.T) {
	// Set gin to test mode
	gin.SetMode(gin.TestMode)

	// Create a test logger
	logger, _ := zap.NewProduction()

	// Create a router with logging middleware
	r := gin.New()
	r.Use(Logging(WithLog(logger), WithLogHeaders()))

	// Add a simple test route
	r.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	var wg sync.WaitGroup
	const numRequests = 50

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(requestID int) {
			defer wg.Done()

			// Create request
			req, _ := http.NewRequest("GET", "/test", nil)
			req.Header.Set("X-Request-ID", "req-"+string(rune(requestID+'0')))
			req.Header.Set("User-Agent", "test-agent")

			// Create response recorder
			w := httptest.NewRecorder()

			// Perform request
			r.ServeHTTP(w, req)

			// Check response
			if w.Code != 200 {
				t.Errorf("Expected status 200, got %d", w.Code)
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrentSensitiveHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger, _ := zap.NewProduction()

	// Create a router with logging middleware that logs headers and has sensitive headers
	r := gin.New()
	r.Use(Logging(
		WithLog(logger),
		WithLogHeaders(),
		WithSensitiveHeaders("authorization", "cookie", "x-api-key"),
	))

	r.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	var wg sync.WaitGroup
	const numRequests = 20

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(requestID int) {
			defer wg.Done()

			req, _ := http.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer token"+string(rune(requestID+'0')))
			req.Header.Set("Cookie", "session=abc"+string(rune(requestID+'0')))
			req.Header.Set("X-API-Key", "key"+string(rune(requestID+'0')))
			req.Header.Set("X-Custom-Header", "custom"+string(rune(requestID+'0'))) // This should be logged

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != 200 {
				t.Errorf("Expected status 200, got %d", w.Code)
			}
		}(i)
	}

	wg.Wait()
}

func BenchmarkConcurrentLogging(b *testing.B) {
	gin.SetMode(gin.TestMode)
	logger, _ := zap.NewProduction()

	r := gin.New()
	r.Use(Logging(WithLog(logger)))

	r.GET("/benchmark", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, _ := http.NewRequest("GET", "/benchmark", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
		}
	})
}
