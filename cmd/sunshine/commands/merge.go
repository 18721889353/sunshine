package commands

import (
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/merge"
)

// MergeCommand merge the generated code
func MergeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge",
		Short: "合并生成的代码到模板文件",
		Long: `合并生成的代码到模板文件，不会影响已编写的逻辑代码。
合并前会自动备份到 /tmp/sunshine_merge_backup_code 目录。`,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(
		merge.GinHandlerCode(),
		merge.GinServiceCode(),
		merge.GRPCServiceCode(),
	)

	return cmd
}
