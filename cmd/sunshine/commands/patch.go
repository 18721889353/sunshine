package commands

import (
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/patch"
)

// PatchCommand patch server code
func PatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "patch",
		Short:         "修补生成的代码",
		Long:          `修补生成的代码。`,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(
		patch.DeleteJSONOmitemptyCommand(),
		patch.GenerateDBInitCommand(),
		patch.GenTypesPbCommand(),
		patch.CopyProtoCommand(),
		patch.CopyThirdPartyProtoCommand(),
		patch.CopyGOModCommand(),
		patch.ModifyDuplicateNumCommand(),
		patch.ModifyDuplicateErrCodeCommand(),
		patch.AdaptMonoRepoCommand(),
		patch.ModifyProtoPackageCommand(),
	)

	return cmd
}
