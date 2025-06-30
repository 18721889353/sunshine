package middleware

import (
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/18721889353/sunshine/pkg/container/group"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/shield/circuitbreaker"
	"github.com/18721889353/sunshine/pkg/utils"
)

func runCircuitBreakerHTTPServer() string {
	serverAddr, requestAddr := utils.GetLocalHTTPAddrPairs()

	degradeHandler := func(c *gin.Context) {
		response.Output(c, http.StatusOK, "degrade")
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(CircuitBreaker(WithGroup(group.NewGroup(func() interface{} {
		return circuitbreaker.NewBreaker()
	})),
		WithValidCode(http.StatusForbidden),
		WithDegradeHandler(degradeHandler),
	))

	r.GET("/hello", func(c *gin.Context) {
		if rand.Int()%2 == 0 {
			response.Output(c, http.StatusInternalServerError)
		} else {
			response.Success(c, "localhost"+serverAddr)
		}
	})

	go func() {
		err := r.Run(serverAddr)
		if err != nil {
			panic(err)
		}
	}()

	time.Sleep(time.Millisecond * 200)
	return requestAddr
}
