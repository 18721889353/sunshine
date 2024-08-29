// Package main is the grpc gateway server of the application.
package main

import (
	"github.com/18721889353/sunshine/pkg/app"

	"github.com/18721889353/sunshine/cmd/serverNameExample_grpcGwPbExample/initial"
)

func main() {
	initial.InitApp()
	services := initial.CreateServices()
	closes := initial.Close(services)

	a := app.New(services, closes)
	a.Run()
}
