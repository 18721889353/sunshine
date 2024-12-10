package commands

import (
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

const latestVersion = "latest"

// InitCommand initial sunshine
func InitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize sunshine",
		Long:  "Initialize sunshine.",
		Example: color.HiBlackString(`  # Run init, download code and install plugins.
  sunshine init`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			targetVersion := latestVersion
			// download sunshine template code
			_, err := runUpgrade(targetVersion)
			if err != nil {
				return err
			}

			// installing dependency plugins
			_, lackNames := checkInstallPlugins()
			installPlugins(lackNames)

			return nil
		},
	}

	return cmd
}
