// Package commands are subcommands of the sunshine command.
package commands

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"
)

var (
	version     = "v0.0.0"
	versionFile = GetSunshineDir() + "/.sunshine/.github/version"
)

// NewRootCMD command entry
func NewRootCMD() *cobra.Command {
	cmd := &cobra.Command{
		Use: "sunshine",
		Long: fmt.Sprintf(`Sunshine is a powerful Go development framework, it's easy to develop web and microservice projects.
Repo: %s
Docs: %s`, color.HiCyanString("https://github.com/18721889353/sunshine"), color.HiCyanString("https://go-sunshine.com")),
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       getVersion(),
	}

	cmd.AddCommand(
		InitCommand(),
		UpgradeCommand(),
		PluginsCommand(),
		GenWebCommand(),
		GenMicroCommand(),
		generate.ConfigCommand(),
		OpenUICommand(),
		MergeCommand(),
		PatchCommand(),
		GenGraphCommand(),
		TemplateCommand(),
	)

	return cmd
}

func getVersion() string {
	data, _ := os.ReadFile(versionFile)
	v := string(data)
	if v != "" {
		return v
	}
	return "unknown, execute command \"sunshine init\" to get version"
}

// GetSunshineDir get sunshine home directory
func GetSunshineDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("can't get home directory'")
		return ""
	}

	return dir
}
