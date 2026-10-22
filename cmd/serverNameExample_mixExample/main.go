// Package main is the http and grpc server of the application.
package main

import (
	"flag"

	"github.com/18721889353/sunshine/pkg/app"

	"github.com/18721889353/sunshine/cmd/serverNameExample_mixExample/initial"
)

// @title serverNameExample api docs
// @description http server api docs
// @schemes http https
// @version 2.0
// @host localhost:8080
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type Bearer your-jwt-token to Value
func main() {
	flag.Parse() // 参数解析留在进程入口：-h 与非法参数的退出在这里发生

	initial.InitApp()
	services := initial.CreateServices()
	closes := initial.Close(services)

	a := app.New(services, closes)
	a.Run()
}
