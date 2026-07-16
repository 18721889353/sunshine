package patch

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gofile"
)

// CopyGOModCommand copy go mod files
func CopyGOModCommand() *cobra.Command {
	var (
		moduleName     string // module name for go.mod
		outPath        string // output directory
		isLogExist     bool
		isForceReplace bool
	)

	cmd := &cobra.Command{
		Use:   "copy-go-mod",
		Short: "复制 go.mod 文件",
		Long:  "复制 go.mod 和 go.sum 文件到指定目录。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：复制 go.mod 文件到当前目录
  # =====================================================================
  sunshine patch copy-go-mod \
    --module-name=yourModuleName \
    --out=./yourServerDir \
    --is-force-replace=false \
    --is-log-exist=false


  # =====================================================================
  # 参数说明：
  #   --module-name     Go 模块名（必填），对应 go.mod 中的 module 声明
  #   --out             输出目录（可选，默认当前目录）
  #   --is-force-replace 是否强制替换已存在的 go.mod 文件（可选，默认 false）
  #   --is-log-exist    是否输出文件已存在的日志（可选，默认 false）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if moduleName == "" {
				return errors.New("module-name is required, please use --module-name to set it")
			}

			goModFile := outPath + gofile.GetPathDelimiter() + "go.mod"
			if gofile.IsExists(goModFile) {
				if !isForceReplace {
					if isLogExist {
						fmt.Printf("%s already exists, skip copying.\n", goModFile)
					}
					return nil
				}
				// delete the go.mod and go.sum file if it exists
				if err := os.RemoveAll(goModFile); err != nil {
					return err
				}
				if err := os.RemoveAll(strings.TrimSuffix(goModFile, ".mod") + ".sum"); err != nil {
					return err
				}
			}

			out, err := runCopyGoModCommand(moduleName, outPath)
			if err != nil {
				return err
			}
			fmt.Printf("copied go.mod to %s\n", out)

			return nil
		},
	}

	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "Go 模块名，对应 go.mod 文件中的 module 声明")
	cmd.Flags().StringVarP(&outPath, "out", "o", ".", "输出目录")
	cmd.Flags().BoolVarP(&isLogExist, "is-log-exist", "l", false, "是否输出文件已存在日志")
	cmd.Flags().BoolVarP(&isForceReplace, "is-force-replace", "f", false, "是否强制替换已存在的 go.mod 文件")

	return cmd
}

func runCopyGoModCommand(moduleName string, out string) (string, error) {
	r := generate.Replacers[generate.TplNameSunshine]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// setting up template information
	subFiles := []string{"sunshine/go.mod", "sunshine/go.sum"}
	r.SetSubDirsAndFiles(nil, subFiles...)
	r.SetReplacementFields(generate.GetGoModFields(moduleName))
	if err := r.SetOutputDir(out); err != nil {
		return "", err
	}
	if err := r.SaveFiles(); err != nil {
		return "", err
	}

	return r.GetOutputDir(), nil
}
