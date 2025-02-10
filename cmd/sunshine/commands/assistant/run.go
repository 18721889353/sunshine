package assistant

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	chatgpt "github.com/18721889353/sunshine/pkg/aicli/chatgpt"
	"github.com/18721889353/sunshine/pkg/aicli/deepseek"
	"github.com/18721889353/sunshine/pkg/utils"
)

const (
	typeChatGPT  = "chatgpt"
	typeDeepSeek = "deepseek"
)

var assistantTypeMap = map[string]string{
	typeChatGPT:  "ChatGPT",
	typeDeepSeek: "DeepSeek",
}

// RunCommand run assistant command
func RunCommand() *cobra.Command {
	var (
		assistantType string
		apiKey        string
		model         string
		role          string
		maxToken      int
		temperature   float32
		useContext    bool
	)

	//nolint
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Running assistant",
		Long:  "Running assistant.",
		Example: color.HiBlackString(` # Running ChatGPT assistant.
  sponge assistant run --type=chatgpt --api-key=your-api-key

  # Running DeepSeek assistant with model.
  sponge assistant run --type=deepseek --api-key=your-api-key --model=deepseek-reasoner`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var opts []chatgpt.ClientOption
			if model != "" {
				opts = append(opts, chatgpt.WithModel(model))
			}
			if maxToken > 0 {
				opts = append(opts, chatgpt.WithMaxTokens(maxToken))
			}
			if temperature > 0 {
				opts = append(opts, chatgpt.WithTemperature(temperature))
			}
			if role != "" {
				opts = append(opts, chatgpt.WithRole(role))
			}
			opts = append(opts, chatgpt.WithUseContext(useContext))

			var client *chatgpt.Client
			var err error
			switch strings.ToLower(assistantType) {
			case typeChatGPT:
				client, err = chatgpt.NewClient(apiKey, opts...)
				if err != nil {
					return err
				}
			case typeDeepSeek:
				client, err = deepseek.NewClient(apiKey, opts...)
				if err != nil {
					return err
				}
			default:
				return cmd.Usage()
			}

			err = dialogueSession(assistantType, client)
			if err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&assistantType, "type", "t", "", "assistant type, e.g. chatgpt, deepseek")
	_ = cmd.MarkFlagRequired("type")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "assistant api key")
	_ = cmd.MarkFlagRequired("api-key")
	cmd.Flags().StringVarP(&model, "model", "m", "", "assistant model, corresponding assistant type.")
	cmd.Flags().StringVarP(&role, "role", "r", "", "role of the model")
	cmd.Flags().IntVarP(&maxToken, "max-token", "s", 0, "maximum number of tokens")
	cmd.Flags().Float32VarP(&temperature, "temperature", "e", 0, "temperature of the model")
	cmd.Flags().BoolVarP(&useContext, "use-context", "c", true, "whether the assistant supports context")

	return cmd
}

func dialogueSession(assistantType string, client *chatgpt.Client) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Enter questions and talk to the assistant, enter 'q' or 'quit' to exit, enter 'r' to refresh context.")
	for {
		fmt.Print(color.HiCyanString("Prompt: "))

		input, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("reading input error: %v", err)
		}
		input = strings.TrimSpace(input)
		fmt.Println()

		if input == "q" || input == "quit" || input == "exit" {
			fmt.Println("Exited assistant.")
			break
		}
		if input == "r" || input == "R" {
			client.RefreshContext()
			fmt.Println(color.HiBlackString("Finished refreshing context.") + "\n\n")
			continue
		}

		answer := client.SendStream(context.Background(), input)
		p := utils.NewWaitPrinter(time.Millisecond * 200)
		p.LoopPrint(assistantTypeMap[assistantType] + ": ")
		isFirst := true
		for content := range answer.Content {
			if isFirst {
				isFirst = false
				p.StopPrint(content)
			} else {
				fmt.Print(content)
			}
		}
		if answer.Err != nil {
			return fmt.Errorf("error : %v", answer.Err)
		}
		fmt.Printf("\n\n\n")
	}
	return nil
}
