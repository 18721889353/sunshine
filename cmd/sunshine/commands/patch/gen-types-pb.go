package patch

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"
	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/replacer"
)

// GenTypesPbCommand generate types.proto code
func GenTypesPbCommand() *cobra.Command {
	var (
		moduleName string // go.mod module name
		outPath    string // output directory
		targetFile = "api/types/types.proto"
	)

	cmd := &cobra.Command{
		Use:   "gen-types-pb",
		Short: "生成 types.proto 代码",
		Long:  "生成 types.proto 代码。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：生成 types.proto 代码
  # 执行后会生成: api/types/types.proto
  # =====================================================================
  sunshine patch gen-types-pb \
    --module-name=yourModuleName \
    --out=./yourServerDir


  # =====================================================================
  # 参数说明：
  #   --module-name  Go 模块名（必填），对应 go.mod 中的 module 声明
  #   --out          输出目录（可选），默认为 ./types-pb_<时间戳>
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if moduleName == "" {
				return fmt.Errorf(`required flag(s) "module-name" not set, use "sunshine patch gen-types-pb -h" for help`)
			}

			var isEmpty bool
			if outPath == "" {
				isEmpty = true
			} else {
				isEmpty = false
				if gofile.IsExists(targetFile) {
					fmt.Printf("'%s' already exists, no need to generate it.\n", targetFile)
					return nil
				}
			}

			var err error
			outPath, err = runTypesPbCommand(moduleName, outPath)
			if err != nil {
				return err
			}

			if isEmpty {
				fmt.Printf(`
using help:
  move the folder "api" to your project code folder.

`)
			}
			if gofile.IsWindows() {
				targetFile = "\\" + strings.ReplaceAll(targetFile, "/", "\\")
			} else {
				targetFile = "/" + targetFile
			}
			fmt.Printf("generate \"types-pb\" code successfully, out = %s\n", cutPathPrefix(outPath+targetFile))
			return nil
		},
	}

	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "Go 模块名，对应 go.mod 文件中的 module 声明")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./types-pb_<时间戳>，"+
		"如果指定为 sunshine 生成的 web 或 micro 服务目录，则可忽略 module-name 参数")

	return cmd
}

func runTypesPbCommand(moduleName string, outPath string) (string, error) {
	subTplName := "types-pb"
	r := generate.Replacers[generate.TplNameSunshine]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// setting up template information
	subDirs := []string{"api/types"} // only the specified subdirectory is processed, if empty or no subdirectory is specified, it means all files
	ignoreDirs := []string{}         // specify the directory in the subdirectory where processing is ignored
	ignoreFiles := []string{         // specify the files in the subdirectory to be ignored for processing
		"types.pb.go", "types.pb.validate.go",
	}

	r.SetSubDirsAndFiles(subDirs)
	r.SetIgnoreSubDirs(ignoreDirs...)
	r.SetIgnoreSubFiles(ignoreFiles...)
	fields := addTypePbFields(moduleName)
	r.SetReplacementFields(fields)
	if err := r.SetOutputDir(outPath, subTplName); err != nil {
		return "", err
	}
	if err := r.SaveFiles(); err != nil {
		return "", err
	}

	return r.GetOutputDir(), nil
}

func addTypePbFields(moduleName string) []replacer.Field {
	var fields []replacer.Field

	fields = append(fields, []replacer.Field{
		{
			Old:             "github.com/18721889353/sunshine",
			New:             moduleName,
			IsCaseSensitive: false,
		},
	}...)

	return fields
}
