package patch

import (
	"errors"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"
	"github.com/18721889353/sunshine/pkg/gofile"
)

// CopyThirdPartyProtoCommand copy third-party proto files
func CopyThirdPartyProtoCommand() *cobra.Command {
	var (
		outPath    string // output directory
		isLogExist bool
	)

	cmd := &cobra.Command{
		Use:   "copy-third-party-proto",
		Short: "复制第三方 proto 文件",
		Long:  "复制第三方 proto 文件到指定目录。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：复制第三方 proto 文件
  # =====================================================================
  sunshine patch copy-third-party-proto \
    --out=./yourServerDir \
    --is-log-exist=false


  # =====================================================================
  # 参数说明：
  #   --out          输出目录（可选，默认当前目录）
  #   --is-log-exist 是否输出文件已存在日志（可选，默认 false）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			out := outPath + gofile.GetPathDelimiter() + "third_party"
			if gofile.IsExists(out) {
				if isLogExist {
					fmt.Printf("%s proto files already exists, skip copying.\n", out)
				}
				return nil
			}

			var err error
			out, err = runCopyThirdPartyProtoCommand(outPath)
			if err != nil {
				return err
			}
			fmt.Printf("copied third_party proto files to %s\n", out)

			return nil
		},
	}

	cmd.Flags().StringVarP(&outPath, "out", "o", ".", "输出目录")
	cmd.Flags().BoolVarP(&isLogExist, "is-log-exist", "l", false, "是否输出文件已存在日志")

	return cmd
}

func runCopyThirdPartyProtoCommand(out string) (string, error) {
	r := generate.Replacers[generate.TplNameSunshine]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// setting up template information
	subDirs := []string{"sunshine/third_party"}

	r.SetSubDirsAndFiles(subDirs)
	if err := r.SetOutputDir(out); err != nil {
		return "", err
	}
	if err := r.SaveFiles(); err != nil {
		return "", err
	}

	return r.GetOutputDir(), nil
}
