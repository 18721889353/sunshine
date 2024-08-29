// Package main sunshine is a basic development framework that integrates code auto generation,
// Gin and GRPC, a microservice framework. it is easy to build a complete project from development
// to deployment, just fill in the business logic code on the generated template code, greatly improved
// development efficiency and reduced development difficulty, the use of Go can also be "low-code development".
package main

import (
	"fmt"
	"os"

	"github.com/18721889353/sunshine/pkg/gofile"

	"github.com/18721889353/sunshine/cmd/sunshine/commands"
	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"
)

func main() {
	err := generate.Init(generate.TplNameSunshine, commands.GetSunshineDir()+gofile.GetPathDelimiter()+".sunshine")
	if err != nil {
		fmt.Printf("\n    %v\n\n", err)
		return
	}

	rootCMD := commands.NewRootCMD()
	if err = rootCMD.Execute(); err != nil {
		rootCMD.PrintErrln("Error:", err)
		os.Exit(1)
	}
}
