package merge

import (
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// GRPCServiceCode merge the grpc service code
func GRPCServiceCode() *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "rpc-pb",
		Short: "合并生成的 gRPC 相关代码到模板文件",
		Long:  "合并生成的 gRPC 相关代码到模板文件。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：合并 gRPC 代码到模板文件
  # 会自动备份到 /tmp/sunshine_merge_backup_code
  # =====================================================================
  sunshine merge rpc-pb \
    --dir=/path/to/server/directory


  # =====================================================================
  # 参数说明：
  #   --dir   输入目录（可选，默认当前目录）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			dir = adaptDir(dir)
			mergeGRPCECode(dir)
			mergeGRPCServiceTmpl(dir)
			return nil
		},
	}

	cmd.Flags().StringVarP(&dir, "dir", "d", ".", "输入目录")

	return cmd
}
