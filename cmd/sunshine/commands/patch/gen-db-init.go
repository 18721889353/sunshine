package patch

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"
	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/replacer"
)

// GenerateDBInitCommand generate database initialization code
func GenerateDBInitCommand() *cobra.Command {
	var (
		moduleName string // go.mod module name
		dbDriver   string // database driver e.g. mysql
		outPath    string // output directory
		targetFile = "internal/database/init.go"
	)

	cmd := &cobra.Command{
		Use:   "gen-db-init",
		Short: "生成数据库初始化代码",
		Long:  "生成数据库初始化代码。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：生成数据库初始化代码
  # 执行后会生成: internal/database/init.go
  # =====================================================================
  sunshine patch gen-db-init \
    --module-name=yourModuleName \
    --db-driver=mysql \
    --out=./yourServerDir


  # =====================================================================
  # 参数说明：
  #   --module-name  Go 模块名（必填），对应 go.mod 中的 module 声明
  #   --db-driver    数据库驱动类型（可选），当前支持 mysql，为空则自动检测
  #   --out          输出目录（可选），默认为 ./mysql-init_<时间戳>
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if outPath == "./" {
				outPath = "."
			}
			if moduleName == "" {
				return fmt.Errorf(`required flag(s) "module-name" not set, use "sunshine patch gen-db-init -h" for help`)
			}

			var isEmpty bool
			if outPath == "" {
				isEmpty = true
			} else {
				isEmpty = false
				initFile := outPath + "/" + targetFile
				if gofile.IsExists(initFile) {
					fmt.Printf("initialization code (%s) already exists, no need to generate it.\n", initFile)
					return nil
				}
			}

			if dbDriver == "" {
				// check handler and service directory db driver mark
				dbDriver = detectDbDriverName()
				if dbDriver == "" {
					fmt.Printf("no database driver found, ignored to generate initialization database code.\n")
					return nil
				}
			}

			g := &dbInitGenerator{
				moduleName: moduleName,
				dbDriver:   dbDriver,
				outPath:    outPath,
			}
			var err error
			outPath, err = g.generateCode()
			if err != nil {
				return err
			}

			if isEmpty {
				fmt.Printf(`
using help:
  move the folder "internal" to your project code folder.

`)
			}
			fmt.Printf("generate \"%s\" initialization code successfully, out = %s\n", dbDriver, outPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&dbDriver, "db-driver", "k", "", "数据库驱动类型，当前支持 mysql")
	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "Go 模块名，对应 go.mod 文件中的 module 声明")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./mysql-init_<时间戳>，"+
		"如果指定为 sunshine 生成的 web 或 micro 服务目录，则可忽略 module-name 参数")

	return cmd
}

type dbInitGenerator struct {
	moduleName string
	dbDriver   string
	outPath    string

	serverName     string
	suitedMonoRepo bool
}

func (g *dbInitGenerator) generateCode() (string, error) {
	subTplName := "init_" + g.dbDriver
	r := generate.Replacers[generate.TplNameSunshine]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	subDirs := []string{}
	selectFiles := map[string][]string{}
	err := generate.SetSelectFiles(g.dbDriver, selectFiles)
	if err != nil {
		return "", err
	}
	if len(selectFiles) == 0 {
		return "", errors.New("no files to generate")
	}

	r.SetSubDirsAndFiles(subDirs, getSubFiles(selectFiles)...)
	if err := r.SetOutputDir(g.outPath, subTplName); err != nil {
		return "", err
	}
	fields := g.addFields(r)
	r.SetReplacementFields(fields)
	if err := r.SaveFiles(); err != nil {
		return "", err
	}

	return r.GetOutputDir(), nil
}

func (g *dbInitGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	fields = append(fields, generate.DeleteCodeMark(r, generate.ModelInitDBFile, generate.StartMark, generate.EndMark)...)
	fields = append(fields, []replacer.Field{
		{
			Old:             "github.com/18721889353/sunshine/internal",
			New:             g.moduleName + "/internal",
			IsCaseSensitive: false,
		},
		{
			Old:             "github.com/18721889353/sunshine/configs",
			New:             g.moduleName + "/configs",
			IsCaseSensitive: false,
		},
		{ // replace the contents of the model/init.go file
			Old: generate.ModelInitDBFileMark,
			New: generate.GetInitDataBaseCode(g.dbDriver),
		},
	}...)

	if g.suitedMonoRepo {
		fs := generate.SubServerCodeFields(g.moduleName, g.serverName)
		fields = append(fields, fs...)
	}

	return fields
}

func getContentMark(dbDriver string) []byte {
	return []byte(generate.CurrentDbDriver(dbDriver))
}

func checkDbDriver(files []string) string {
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		if bytes.Contains(data, getContentMark(generate.DBDriverMysql)) {
			return generate.DBDriverMysql
		}
	}
	return ""
}

func detectDbDriverName() string {
	var dbDriverName string
	files, err := gofile.ListFiles("internal/handler", gofile.WithSuffix(".go"))
	if err == nil && len(files) > 0 {
		dbDriverName = checkDbDriver(files)
		if dbDriverName != "" {
			return dbDriverName
		}
	}

	files, err = gofile.ListFiles("internal/service", gofile.WithSuffix(".go"))
	if err == nil && len(files) > 0 {
		dbDriverName = checkDbDriver(files)
	}
	return dbDriverName
}
