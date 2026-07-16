package generate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// GRPCConnectionCommand generate grpc connection code
func GRPCConnectionCommand() *cobra.Command {
	var (
		moduleName      string // module name for go.mod
		outPath         string // output directory
		grpcServerNames string // grpc service names

		serverName     string // server name
		suitedMonoRepo bool   // whether the generated code is suitable for mono-repo
	)

	cmd := &cobra.Command{
		Use:   "rpc-conn",
		Short: "生成 gRPC 连接代码",
		Long:  "生成 gRPC 连接代码，用于跨服务调用。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：生成 gRPC 连接代码
  # 执行后会生成: internal/rpcclient/（gRPC 客户端连接）
  # =====================================================================
  sunshine micro rpc-conn \
    --module-name=yourModuleName \
    --rpc-server-name=user \
    --server-name=yourServerName \
    --suited-mono-repo=false \
    --out=./yourServerDir


  # =====================================================================
  # 参数说明：
  #   --module-name     Go 模块名（必填），对应 go.mod 中的 module 声明
  #   --rpc-server-name gRPC 服务名（必填），多个用逗号分隔
  #   --server-name     服务名（mono-repo 模式必填）
  #   --suited-mono-repo 是否适配单体仓库结构（可选，默认 false）
  #   --out             输出目录（可选，默认 ./rpc-conn_<时间戳>）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if moduleName == "" {
				return errors.New(`required flag(s) "module-name" not set, use "sunshine micro rpc-conn -h" for help`)
			}
			if suitedMonoRepo {
				if serverName == "" {
					return errors.New(`required flag(s) "server-name" not set, use "sunshine micro rpc-conn -h" for help`)
				}
				serverName = convertServerName(serverName)
				outPath = changeOutPath(outPath, serverName)
			}

			grpcNames := strings.Split(grpcServerNames, ",")
			for _, grpcName := range grpcNames {
				if grpcName == "" {
					continue
				}

				var err error
				var g = &grpcConnectionGenerator{
					moduleName: moduleName,
					grpcName:   grpcName,
					outPath:    outPath,

					serverName:     serverName,
					suitedMonoRepo: suitedMonoRepo,
				}
				outPath, err = g.generateCode()
				if err != nil {
					return err
				}
			}

			fmt.Printf(`
using help:
  move the folder "internal" to your project code folder.

`)
			fmt.Printf("generate \"rpc-conn\" code successfully, out = %s\n", outPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "Go 模块名，对应 go.mod 文件中的 module 声明")
	cmd.Flags().StringVarP(&grpcServerNames, "rpc-server-name", "r", "", "gRPC 服务名，多个用逗号分隔")
	if err := cmd.MarkFlagRequired("rpc-server-name"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "服务名称")
	cmd.Flags().BoolVarP(&suitedMonoRepo, "suited-mono-repo", "l", false, "是否适配单体仓库结构")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./rpc-conn_<时间戳>，"+flagTip("module-name"))

	return cmd
}

type grpcConnectionGenerator struct {
	moduleName string
	grpcName   string
	outPath    string

	serverName     string
	suitedMonoRepo bool
}

// generateCode 生成 gRPC 连接代码
func (g *grpcConnectionGenerator) generateCode() (string, error) {
	subTplName := codeNameGRPCConn
	r := Replacers[TplNameSunshine]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// 指定子目录和文件
	var subDirs []string
	subFiles := []string{"internal/rpcclient/serverNameExample.go"}

	r.SetSubDirsAndFiles(subDirs, subFiles...)
	// 设置输出目录
	if err := r.SetOutputDir(g.outPath, subTplName); err != nil {
		return "", err
	}
	// 构建字段替换规则
	fields := g.addFields(r)
	// 应用替换规则
	r.SetReplacementFields(fields)
	// 保存文件
	if err := r.SaveFiles(); err != nil {
		return "", err
	}

	return r.GetOutputDir(), nil
}

// addFields 添加字段替换规则
func (g *grpcConnectionGenerator) addFields(_ replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	// 添加核心字段替换规则
	fields = append(fields, []replacer.Field{
		{
			Old: "github.com/18721889353/sunshine/configs",
			New: g.moduleName + "/configs",
		},
		{
			Old: "github.com/18721889353/sunshine/internal/config",
			New: g.moduleName + "/internal/config",
		},
		{
			Old:             "serverNameExample",
			New:             g.grpcName,
			IsCaseSensitive: true,
		},
	}...)

	// 单体仓库适配
	if g.suitedMonoRepo {
		fs := SubServerCodeFields(g.moduleName, g.serverName)
		fields = append(fields, fs...)
	}

	return fields
}
