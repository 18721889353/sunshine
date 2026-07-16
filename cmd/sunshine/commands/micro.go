package commands

import (
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"
)

// GenMicroCommand generate micro service code
func GenMicroCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "micro",
		Short:         "生成微服务代码（protobuf/model/cache/dao/service/grpc 等）",
		Long:          "生成微服务代码，包含 protobuf、model、cache、dao、service、grpc、grpc-gw、grpc+http 等。",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(
		generate.ProtobufCommand(),
		generate.ModelCommand("micro"),
		generate.DaoCommand("micro"),
		generate.CacheCommand("micro"),
		generate.ServiceCommand(),
		generate.RPCCommand(),
		generate.RPCGwPbCommand(),
		generate.RPCPbCommand(),
		generate.GRPCConnectionCommand(),
		generate.ConvertSwagJSONCommand("micro"),
		generate.GRPCAndHTTPPbCommand(),
		generate.ServiceAndHandlerCRUDCommand(),
	)

	return cmd
}
