// Package main sunshine is a basic development framework that integrates code auto generation,
// Gin and GRPC, a microservice framework. it is easy to build a complete project from development
// to deployment, just fill in the business logic code on the generated template code, greatly improved
// development efficiency and reduced development difficulty, the use of Go can also be "low-code development".
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/18721889353/sunshine/cmd/sunshine/commands"
	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"
)

func main() {
	// 检测是否是编译后的二进制运行（非 go run 临时编译）
	// go run 的可执行文件在临时目录，包含 "go-build"
	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("get executable path error: %v\n", err)
		return
	}
	isGoRun := strings.Contains(exePath, os.TempDir()) ||
		strings.Contains(exePath, "go-build")

	if !isGoRun {
		// 编译后的二进制，不添加 replace 指令（用户独立项目）
		if setErr := os.Setenv("SUNSHINE_COMPILED_BINARY", "true"); setErr != nil {
			fmt.Printf("set env error: %v\n", setErr)
		}
	}
	// go run 是本地调试模式，会添加 replace 指令

	if initErr := generate.Init(); initErr != nil {
		fmt.Printf("\n    %v\n\n", initErr)
		return
	}

	rootCMD := commands.NewRootCMD()
	if execErr := rootCMD.Execute(); execErr != nil {
		rootCMD.PrintErrln("Error:", execErr)
		os.Exit(1)
	}
}
