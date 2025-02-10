package commands

import (
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/assistant"
)

// AssistantCommand AI assistant command
func AssistantCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "assistant",
		Short:         "AI assistants, support ChatGPT and DeepSeek",
		Long:          "AI assistant, supports ChatGPT and DeepSeek, defaults to using ChatGPT.",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(
		assistant.RunCommand(),
		assistant.GenerateCommand(),
	)
	return cmd
}
