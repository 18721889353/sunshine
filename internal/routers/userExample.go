package routers

import (
	"github.com/gin-gonic/gin"

	"github.com/18721889353/sunshine/internal/handler"
)

func init() {
	apiV1RouterFns = append(apiV1RouterFns, func(group *gin.RouterGroup) {
		userExampleRouter(group, handler.NewUserExampleHandler())
	})
}

func userExampleRouter(group *gin.RouterGroup, h handler.UserExampleHandler) {
	//group.Use(middleware.Auth()) // all of the following routes use jwt authentication
	// or group.Use(middleware.Auth(middleware.WithVerify(verify))) // token authentication
	g := group.Group("/userExample")

	// All the following routes use jwt authentication, you also can use middleware.Auth(middleware.WithVerify(fn))
	//g.Use(middleware.Auth())

	// If jwt authentication is not required for all routes, authentication middleware can be added
	// separately for only certain routes. In this case, g.Use(middleware.Auth()) above should not be used.
	//g.GET("/:id", h.GetByID,middleware.Auth())       // [get] /api/v1/userExample/:id

	g.POST("/", h.Create)          // [post] /api/v1/userExample
	g.DELETE("/:id", h.DeleteByID) // [delete] /api/v1/userExample/:id
	g.PUT("/:id", h.UpdateByID)    // [put] /api/v1/userExample/:id
	g.GET("/:id", h.GetByID)       // [get] /api/v1/userExample/:id
	g.POST("/list", h.List)        // [post] /api/v1/userExample/list
}
