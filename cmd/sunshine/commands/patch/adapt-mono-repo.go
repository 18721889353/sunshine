package patch

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gofile"
)

// AdaptMonoRepoCommand Adapt to mono-repo command
func AdaptMonoRepoCommand() *cobra.Command {
	var (
		dir        string
		moduleName string // module name for go.mod
		serverName string // server name
	)

	cmd := &cobra.Command{
		Use:   "adapt-mono-repo",
		Short: "适配 api 目录代码到单体仓库模式",
		Long:  "适配 api 目录代码到单体仓库模式。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：适配 api 目录代码到单体仓库模式
  # =====================================================================
  sunshine patch adapt-mono-repo \
    --dir=/path/to/server/directory


  # =====================================================================
  # 参数说明：
  #   --dir   输入目录（可选，默认当前目录）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if moduleName == "" {
				return errors.New(`can't get info from docs/gen.info`)
			}
			if serverName == "" {
				return errors.New(`can't get info from docs/gen.info`)
			}

			files, err := gofile.ListFiles(dir, gofile.WithSuffix(".go"))
			if err != nil {
				return err
			}

			var oldStr = fmt.Sprintf("\"%s/api", moduleName+"/"+serverName)
			var newStr = fmt.Sprintf("\"%s/api", moduleName)
			for _, file := range files {
				data, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				if bytes.Contains(data, []byte(oldStr)) {
					data = bytes.ReplaceAll(data, []byte(oldStr), []byte(newStr))
					err = os.WriteFile(file, data, 0766)
					if err != nil {
						return err
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&dir, "dir", "d", ".", "输入目录")

	return cmd
}
