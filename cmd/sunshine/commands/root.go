// Package commands 包含 sunshine 命令的所有子命令。
package commands

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"
)

// 定义版本号变量
var (
	version     = "v0.0.0"
	versionFile = GetSunshineDir() + "/.sunshine/.github/version"
)

// NewRootCMD 创建并返回 sunshine 命令的根命令实例
func NewRootCMD() *cobra.Command {
	// 创建一个名为 "sunshine" 的 cobra 命令实例
	cmd := &cobra.Command{
		// 定义命令的名称
		Use: "sunshine",
		// 定义命令的详细描述信息
		Long: fmt.Sprintf(
			`Sunshine 是一个强大的 Go 开发框架，适合开发 Web 和微服务项目。
仓库地址: %s
文档地址: %s`,
			color.HiCyanString("https://github.com/18721889353/sunshine"),
			color.HiCyanString("https://go-sunshine.com"),
		),
		// 设置为静默错误输出
		SilenceErrors: true,
		// 设置为静默使用信息输出
		SilenceUsage: true,
		// 设置命令版本信息
		Version: getVersion(),
	}

	// 添加各个子命令到根命令
	cmd.AddCommand(
		InitCommand(),            // 初始化命令
		UpgradeCommand(),         // 升级命令
		PluginsCommand(),         // 插件管理命令
		GenWebCommand(),          // 生成 Web 项目命令
		GenMicroCommand(),        // 生成微服务项目命令
		generate.ConfigCommand(), // 生成配置文件命令
		OpenUICommand(),          // 打开 UI 命令
		MergeCommand(),           // 合并命令
		PatchCommand(),           // 补丁命令
		GenGraphCommand(),        // 生成图命令
	)

	return cmd
}

// getVersion 从文件中读取版本号,如果文件不存在或为空,则返回默认版本信息
func getVersion() string {
	data, err := os.ReadFile(versionFile)
	if err != nil {
		// 文件不存在或读取失败,返回编译时嵌入的版本号
		if version != "v0.0.0" {
			return version
		}
		return "unknown, 执行命令 \"sunshine init\" 获取版本信息"
	}
	v := string(data)
	if v != "" {
		return v
	}
	// 文件内容为空,返回编译时嵌入的版本号
	if version != "v0.0.0" {
		return version
	}
	return "unknown, 执行命令 \"sunshine init\" 获取版本信息"
}

// GetSunshineDir 获取 sunshine 的主目录路径
func GetSunshineDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("无法获取主目录")
		return ""
	}

	return dir
}
