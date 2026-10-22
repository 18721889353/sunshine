// Package main is the grpc server of the application.
package main

import (
	"flag"

	"github.com/18721889353/sunshine/pkg/app"

	"github.com/18721889353/sunshine/cmd/serverNameExample_grpcPbExample/initial"
)

func main() {
	flag.Parse() // 参数解析留在进程入口：-h 与非法参数的退出在这里发生

	initial.InitApp()
	services := initial.CreateServices()
	closes := initial.Close(services)

	a := app.New(services, closes)
	a.Run()
}
