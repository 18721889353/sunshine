package merge

import (
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// GinServiceCode merge the gin service code
func GinServiceCode() *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "rpc-gw-pb",
		Short: "Merge the generated grpc gateway related code into the template file",
		Long:  "Merge the generated grpc gateway related code into the template file.",
		Example: color.HiBlackString(`  # Merge go template file in local server directory
  sunshine merge rpc-gw-pb

  # Merge go template file in specified server directory
  sunshine merge rpc-gw-pb --dir=/path/to/server/directory`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			dir = adaptDir(dir)
			mergeGRPCECode(dir)
			mergeGinRouters(dir)
			mergeGRPCServiceClientTmpl(dir)
			return nil
		},
	}

	cmd.Flags().StringVarP(&dir, "dir", "d", ".", "input directory")

	return cmd
}
