package commands

import (
	"github.com/spf13/cobra" // 导入 cobra 库，用于创建命令行工具

	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate" // 导入 generate 包，用于生成各种代码
)

// GenWebCommand 生成 Web 服务器代码的命令
// 该命令用于生成 Web 项目的模型、缓存、数据访问对象、处理器和 HTTP 代码。
func GenWebCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "web",                                             // 命令的使用方式，用户在命令行中输入的命令
		Short:         "Generate model, cache, dao, handler, http code",  // 命令的简短描述
		Long:          "Generate model, cache, dao, handler, http code.", // 命令的详细描述
		SilenceErrors: true,                                              // 设置为静默错误输出，不显示错误信息
		SilenceUsage:  true,                                              // 设置为静默使用信息输出，不显示使用信息
	}

	// 添加各个子命令到根命令
	cmd.AddCommand(
		generate.ModelCommand("web"),           // 生成模型代码
		generate.DaoCommand("web"),             // 生成数据访问对象 (DAO) 代码
		generate.CacheCommand("web"),           // 生成缓存代码
		generate.HandlerCommand(),              // 生成处理器代码
		generate.HTTPCommand(),                 // 生成 HTTP 代码
		generate.HTTPPbCommand(),               // 生成 HTTP Protocol Buffer 代码
		generate.ConvertSwagJSONCommand("web"), // 将 Swagger JSON 转换为其他格式
		generate.HandlerPbCommand(),            // 生成处理器 Protocol Buffer 代码
	)

	return cmd
}
