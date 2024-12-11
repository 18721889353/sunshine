package commands

import (
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

const latestVersion = "latest"

// InitCommand 创建并返回初始化 sunshine 的命令实例
func InitCommand() *cobra.Command {
	// 创建一个名为 "init" 的 cobra 命令实例
	cmd := &cobra.Command{
		// 定义命令的名称
		Use: "init",
		// 定义命令的简短描述信息
		Short: "初始化 sunshine",
		// 定义命令的详细描述信息
		Long: "初始化 sunshine。",
		// 定义命令的使用示例
		Example: color.HiBlackString(`  # 运行 init，下载代码并安装插件。
  sunshine init`),
		// 设置为静默错误输出
		SilenceErrors: true,
		// 设置为静默使用信息输出
		SilenceUsage: true,
		// 定义命令执行时的操作
		RunE: func(cmd *cobra.Command, args []string) error {
			// 设置目标版本为最新版本
			targetVersion := latestVersion
			// 下载 sunshine 模板代码
			_, err := runUpgrade(targetVersion)
			if err != nil {
				return err
			}

			// 检查并安装缺少的依赖插件
			_, lackNames := checkInstallPlugins()
			installPlugins(lackNames)

			return nil
		},
	}

	return cmd
}
