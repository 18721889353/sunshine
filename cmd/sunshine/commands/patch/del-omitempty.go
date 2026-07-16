package patch

import (
	"bytes"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gofile"
)

// DeleteJSONOmitemptyCommand delete json omitempty
func DeleteJSONOmitemptyCommand() *cobra.Command {
	var (
		dir        string
		suffixName string
	)

	cmd := &cobra.Command{
		Use:   "del-omitempty",
		Short: "删除 JSON 标签中的 omitempty",
		Long:  "删除 JSON 标签中的 omitempty。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：删除 JSON 标签中的 omitempty
  # =====================================================================
  sunshine patch del-omitempty \
    --dir=./api \
    --suffix-name=pb.go


  # =====================================================================
  # 参数说明：
  #   --dir         输入目录（必填）
  #   --suffix-name 指定文件名后缀（可选），为空则处理所有文件
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			err := replaceFiles(dir, suffixName)
			if err != nil {
				return err
			}

			fmt.Printf("delete the json tag omitempty was successful.\n")
			return nil
		},
	}

	cmd.Flags().StringVarP(&dir, "dir", "d", "", "输入目录")
	if err := cmd.MarkFlagRequired("dir"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	cmd.Flags().StringVarP(&suffixName, "suffix-name", "s", "", "指定文件名后缀，为空则处理所有文件")

	return cmd
}

func replaceFiles(dir string, suffixName string) error {
	opt := gofile.WithSuffix(suffixName)
	if suffixName == "" {
		opt = nil
	}
	files, err := gofile.ListFiles(dir, opt)
	if err != nil {
		return err
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		data = bytes.ReplaceAll(data, []byte(`,omitempty"`), []byte(`"`))
		err = os.WriteFile(file, data, 0666)
		if err != nil {
			return err
		}
	}
	return nil
}
