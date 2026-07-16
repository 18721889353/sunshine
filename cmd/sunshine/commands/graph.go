package commands

import (
	"errors"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gobash"
)

// GenGraphCommand generate graph command
func GenGraphCommand() *cobra.Command {
	var (
		isAll      bool
		projectDir string
		serverDir  []string
	)

	cmd := &cobra.Command{
		Use:   "graph",
		Short: "绘制基于 sunshine 创建项目的业务架构图",
		Long:  "绘制基于 sunshine 创建项目的业务架构图。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：绘制业务架构图
  # =====================================================================
  sunshine graph \
    --project-dir=/path/to/project \
    --server-dir=/path/to/server1 \
    --all


  # =====================================================================
  # 参数说明：
  #   --project-dir 项目目录（可选）
  #   --server-dir  服务目录（可选），可设置多个
  #   --all         是否包含数据库等依赖服务（可选，默认 false）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectDir == "" && len(serverDir) == 0 {
				return errors.New("no project directory or server directory specified\n\n" + cmd.Example)
			}

			_, err := gobash.Exec("spograph", "-h")
			if err != nil {
				fmt.Printf("未找到 spograph 命令，请通过以下命令安装: %s\n",
					color.HiCyanString("go install github.com/sunshine/spograph@latest"))
				return nil
			}

			var params []string
			if projectDir != "" {
				params = append(params, "--project-dir="+projectDir)
			}
			for _, dir := range serverDir {
				params = append(params, "--server-dir="+dir)
			}
			if isAll {
				params = append(params, "--all")
			}
			result, err := gobash.Exec("spograph", params...)
			if err != nil {
				return err
			}
			fmt.Printf("%s", string(result))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&isAll, "all", "a", false, "是否包含数据库等依赖服务")
	cmd.Flags().StringVarP(&projectDir, "project-dir", "p", "", "项目目录")
	cmd.Flags().StringSliceVarP(&serverDir, "server-dir", "s", []string{}, "服务目录，可设置多个")

	return cmd
}
