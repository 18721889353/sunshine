package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gobash"
)

// 定义需要检查的插件名称列表
var pluginNames = []string{
	"go",
	"protoc",
	"protoc-gen-go",
	"protoc-gen-go-grpc",
	"protoc-gen-validate",
	"protoc-gen-gotag",
	"protoc-gen-go-gin",
	"protoc-gen-go-rpc-tmpl",
	"protoc-gen-json-field",
	"protoc-gen-openapiv2",
	"protoc-gen-doc",
	"swag",
	//"golangci-lint",
	//"go-callvis",
}

// 定义插件安装命令映射
var installPluginCommands = map[string]string{
	"go":                     "go: 请手动安装，下载地址为 https://go.dev/dl/ 或 https://golang.google.cn/dl/",
	"protoc":                 "protoc: 请手动安装，下载地址为 https://github.com/protocolbuffers/protobuf/releases/tag/v25.2",
	"protoc-gen-go":          "google.golang.org/protobuf/cmd/protoc-gen-go@latest",
	"protoc-gen-go-grpc":     "google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest",
	"protoc-gen-validate":    "github.com/envoyproxy/protoc-gen-validate@latest",
	"protoc-gen-gotag":       "github.com/srikrsna/protoc-gen-gotag@latest",
	"protoc-gen-go-gin":      "github.com/18721889353/sunshine/cmd/protoc-gen-go-gin@latest",
	"protoc-gen-go-rpc-tmpl": "github.com/18721889353/sunshine/cmd/protoc-gen-go-rpc-tmpl@latest",
	"protoc-gen-json-field":  "github.com/18721889353/sunshine/cmd/protoc-gen-json-field@latest",
	"protoc-gen-openapiv2":   "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest",
	"protoc-gen-doc":         "github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc@latest",
	"swag":                   "github.com/swaggo/swag/cmd/swag@v1.8.12",
	//"golangci-lint":          "github.com/golangci/golangci-lint/cmd/golangci-lint@latest",
	//"go-callvis":             "github.com/ofabry/go-callvis@latest",
}

// 定义符号常量
const (
	installedSymbol = "✔ " // 已安装符号
	lackSymbol      = "❌ " // 缺失符号
	warnSymbol      = "⚠ " // 警告符号
)

// PluginsCommand 创建一个管理依赖插件的 Cobra 命令
func PluginsCommand() *cobra.Command {
	var installFlag bool
	var skipPluginName string

	cmd := &cobra.Command{
		Use:   "plugins",
		Short: "管理 sunshine 依赖插件",
		Long:  "管理 sunshine 依赖插件。",
		Example: color.HiBlackString(`  # 显示所有依赖插件。
  sunshine plugins

  # 安装所有依赖插件。
  sunshine plugins --install

  # 跳过安装某些依赖插件，多个插件名称用逗号分隔
  sunshine plugins --install --skip=go-callvis`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 检查已安装和缺失的插件
			installedNames, lackNames := checkInstallPlugins()
			// 根据 skipPluginName 过滤缺失的插件
			lackNames = filterLackNames(lackNames, skipPluginName)
			if installFlag {
				// 如果设置了安装标志，则安装缺失的插件
				installPlugins(lackNames)
			} else {
				// 否则显示依赖插件的状态
				showDependencyPlugins(installedNames, lackNames)
			}

			return nil
		},
	}
	cmd.Flags().BoolVarP(&installFlag, "install", "i", false, "安装依赖插件")
	cmd.Flags().StringVarP(&skipPluginName, "skip", "s", "", "跳过安装依赖插件")

	return cmd
}

// checkInstallPlugins 检查哪些插件已安装，哪些缺失
func checkInstallPlugins() ([]string, []string) {
	var installedNames []string
	var lackNames []string
	for _, name := range pluginNames {
		_, err := gobash.Exec("which", name)
		if err != nil {
			lackNames = append(lackNames, name)
			continue
		}
		installedNames = append(installedNames, name)
	}

	data, _ := os.ReadFile(versionFile)
	v := string(data)
	if v != "" {
		version = v
	}

	return installedNames, lackNames
}

// showDependencyPlugins 显示已安装和缺失的依赖插件
func showDependencyPlugins(installedNames []string, lackNames []string) {
	var content string

	if len(installedNames) > 0 {
		content = "已安装的依赖插件:\n"
		for _, name := range installedNames {
			content += "    " + installedSymbol + " " + name + "\n"
		}
	}

	if len(lackNames) > 0 {
		content += "\n未安装的依赖插件:\n"
		for _, name := range lackNames {
			content += "    " + lackSymbol + " " + name + "\n"
		}
		content += "\n使用命令 sunshine plugins --install 安装依赖插件\n"
	} else {
		content += "\n所有依赖插件已安装。\n"
	}

	fmt.Println(content)
}

// installPlugins 安装缺失的依赖插件
func installPlugins(lackNames []string) {
	if len(lackNames) == 0 {
		fmt.Printf("\n    所有依赖插件已安装。\n\n")
		return
	}
	fmt.Printf("\n正在安装 %d 个依赖插件，请稍等片刻。\n\n", len(lackNames))

	var wg = &sync.WaitGroup{}
	var manuallyNames []string
	for _, name := range lackNames {
		if name == "go" || name == "protoc" {
			manuallyNames = append(manuallyNames, name)
			continue
		}

		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			ctx, _ := context.WithTimeout(context.Background(), time.Minute*3) //nolint
			pkgAddr, ok := installPluginCommands[name]
			if !ok {
				return
			}
			pkgAddr = adaptInternalCommand(name, pkgAddr)
			result := gobash.Run(ctx, "go", "install", pkgAddr)
			for v := range result.StdOut {
				_ = v
			}
			if result.Err != nil {
				fmt.Printf("%s %s, %v\n", lackSymbol, name, result.Err)
			} else {
				fmt.Printf("%s %s\n", installedSymbol, name)
			}
		}(name)
	}

	wg.Wait()

	for _, name := range manuallyNames {
		fmt.Println(warnSymbol + " " + installPluginCommands[name])
	}
	fmt.Println()
}

// adaptInternalCommand 根据版本调整内部命令
func adaptInternalCommand(name string, pkgAddr string) string {
	if name == "protoc-gen-go-gin" || name == "protoc-gen-go-rpc-tmpl" || name == "protoc-gen-json-field" {
		if version != "v0.0.0" {
			return strings.ReplaceAll(pkgAddr, "@latest", "@"+version)
		}
	}

	return pkgAddr
}

// filterLackNames 根据 skipPluginName 过滤缺失的插件名称
func filterLackNames(lackNames []string, skipPluginName string) []string {
	if skipPluginName == "" {
		return lackNames
	}
	skipPluginNames := strings.Split(skipPluginName, ",")

	names := []string{}
	for _, name := range lackNames {
		isMatch := false
		for _, pluginName := range skipPluginNames {
			if name == pluginName {
				isMatch = true
				break
			}
		}
		if !isMatch {
			names = append(names, name)
		}
	}
	return names
}
