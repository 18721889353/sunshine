package patch

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gofile"
)

// ModifyProtoPackageCommand modifies the package and go_package names of proto files.
func ModifyProtoPackageCommand() *cobra.Command {
	var (
		dir        string
		moduleName string
		serverDir  string
	)

	cmd := &cobra.Command{
		Use:   "modify-proto-package",
		Short: "修改 proto 文件的 package 和 go_package 名称",
		Long:  "修改 proto 文件的 package 和 go_package 名称。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：修改 proto 文件的 package 和 go_package 名称
  # =====================================================================
  sunshine patch modify-proto-package \
    --dir=api \
    --module-name=yourModuleName \
    --server-dir=server


  # =====================================================================
  # 参数说明：
  #   --dir         输入目录（必填）
  #   --module-name Go 模块名（必填）
  #   --server-dir  服务目录（可选），从 docs/gen.info 获取模块名
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if moduleName == "" {
				return errors.New("'module-name' is required")
			}

			protoFiles, err := gofile.ListFiles(dir, gofile.WithSuffix(".proto"), gofile.WithNoAbsolutePath())
			if err != nil {
				return err
			}
			if len(protoFiles) == 0 {
				fmt.Printf("no proto files found in the directory '%s'.\n", dir)
				return nil
			}

			var successFiles []string
			for _, file := range protoFiles {
				ss := splitProtoFilePath(gofile.GetDir(file))
				packageName, goPackageName := getPackageName(ss, moduleName)
				err = replaceProtoPackages(file, packageName, goPackageName)
				if err != nil {
					return err
				}
				successFiles = append(successFiles, file)
			}

			if len(successFiles) > 0 {
				fmt.Printf(`modified the package and go_package names of files:
    %s`, strings.Join(successFiles, "\n    "))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&dir, "dir", "d", "", "输入目录")
	if err := cmd.MarkFlagRequired("dir"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	cmd.Flags().StringVarP(&serverDir, "server-dir", "s", "", "服务目录，从 docs/gen.info 获取模块名和服务名")
	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "Go 模块名")

	return cmd
}

func getPackageName(ss []string, moduleName string) (packageName string, goPackageName string) {
	l := len(ss)
	switch l {
	case 0:
		packageName = "v1"
		goPackageName = `"v1"`
		return packageName, goPackageName
	case 1:
		if ss[0] == "." {
			ss[0] = "v1"
		}
		packageName = ss[0]
		goPackageName = fmt.Sprintf(`"%s/%s;%s"`, moduleName, ss[0], ss[0])
		return packageName, goPackageName
	case 2:
		packageName = strings.Join(ss, ".")
		goPackageName = fmt.Sprintf(`"%s/%s;%s"`, moduleName, strings.Join(ss, "/"), ss[1])
		return packageName, goPackageName
	}
	packageName = strings.Join(ss[l-3:], ".")
	goPackageName = fmt.Sprintf(`"%s/%s;%s"`, moduleName, strings.Join(ss, "/"), ss[l-1])
	return packageName, goPackageName
}

func splitProtoFilePath(protoFilePath string) []string {
	ss := strings.Split(protoFilePath, gofile.GetPathDelimiter())
	if len(ss) > 0 {
		if ss[0] == ".." || ss[0] == "." {
			return ss[1:]
		}
	}
	return ss
}

func replaceProtoPackages(protoFilePath, packageName, goPackage string) error {
	data, err := os.ReadFile(protoFilePath)
	if err != nil {
		return err
	}

	if bytes.Contains(data, []byte("\r\n")) {
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}

	regStr2 := `go_package [\w\W]*?;\n`
	reg2 := regexp.MustCompile(regStr2)
	srcGoPackageName := reg2.Find(data)
	newGoPackage := fmt.Sprintf("go_package = %s;\n", goPackage)
	if len(srcGoPackageName) > 0 {
		data = bytes.Replace(data, srcGoPackageName, []byte(newGoPackage), 1)
	} else {
		data = bytes.Replace(data, []byte("\n\n"), []byte("\n\n"+newGoPackage+"\n\n"), 1)
	}

	regStr := `\npackage [\w\W]*?;`
	reg := regexp.MustCompile(regStr)
	srcPackageName := reg.Find(data)
	newPackage := fmt.Sprintf("\npackage %s;", packageName)
	if len(srcPackageName) > 0 {
		data = bytes.Replace(data, srcPackageName, []byte(newPackage), 1)
	} else {
		data = bytes.Replace(data, []byte("\n\n"), []byte("\n\n"+newPackage+"\n\n"), 1)
	}

	return os.WriteFile(protoFilePath, data, 0666)
}
