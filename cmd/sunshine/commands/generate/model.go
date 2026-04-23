package generate

import (
	"errors"  // 导入错误处理包
	"fmt"     // 导入格式化输入输出包
	"strings" // 导入字符串处理包

	"github.com/fatih/color" // 导入颜色处理包，用于美化命令行输出
	"github.com/spf13/cobra" // 导入 cobra 库，用于创建命令行工具

	"github.com/18721889353/sunshine/pkg/replacer"        // 导入替换器包，用于生成代码时的模板替换
	"github.com/18721889353/sunshine/pkg/sql2code"        // 导入 SQL 转代码包，用于从 SQL 生成 Go 代码
	"github.com/18721889353/sunshine/pkg/sql2code/parser" // 导入 SQL 解析器包，用于解析 SQL 语句
)

// ModelCommand 生成模型代码的命令
// 该命令根据 SQL 生成模型代码。
func ModelCommand(parentName string) *cobra.Command {
	var (
		outPath  string // 输出目录
		dbTables string // 数据库表名，多个表名用逗号分隔

		sqlArgs = sql2code.Args{ // SQL 转代码的参数
			Package:  "model", // 生成代码的包名
			JSONTag:  true,    // 是否生成 JSON 标签
			GormType: true,    // 是否生成 GORM 类型
		}
	)

	cmd := &cobra.Command{
		Use:   "model",                             // 命令的使用方式，用户在命令行中输入的命令
		Short: "Generate model code based on sql",  // 命令的简短描述
		Long:  "Generate model code based on sql.", // 命令的详细描述
		Example: color.HiBlackString(fmt.Sprintf(`  # 生成模型代码。
  sunshine %s model --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user

  # 生成多个表的模型代码。
  sunshine %s model --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=t1,t2

  # 生成模型代码并指定输出目录，注意：如果最新生成的文件已经存在，则代码生成将被取消。
  sunshine %s model --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --out=./yourServerDir`,
			parentName, parentName, parentName)),
		SilenceErrors: true, // 设置为静默错误输出，不显示错误信息
		SilenceUsage:  true, // 设置为静默使用信息输出，不显示使用信息
		RunE: func(_ *cobra.Command, _ []string) error {
			tableNames := strings.Split(dbTables, ",") // 将表名字符串按逗号分隔成数组
			for _, tableName := range tableNames {
				if tableName == "" {
					continue // 跳过空表名
				}

				sqlArgs.DBTable = tableName               // 设置当前处理的表名
				codes, err := sql2code.Generate(&sqlArgs) // 生成模型代码
				if err != nil {
					return err // 返回生成代码时的错误
				}

				g := &modelGenerator{
					codes:   codes,   // 生成的代码
					outPath: outPath, // 输出目录
				}
				outPath, err = g.generateCode() // 生成代码并返回输出目录
				if err != nil {
					return err // 返回生成代码时的错误
				}
			}

			fmt.Printf(`
使用帮助:
  将 "internal" 文件夹移动到你的项目代码目录中。

`)
			fmt.Printf("成功生成 \"model\" 代码，输出目录 = %s\n", outPath) // 打印生成成功的消息
			return nil
		},
	}

	cmd.Flags().StringVarP(&sqlArgs.DBDriver, "db-driver", "k", "mysql", "数据库驱动，支持 mysql")
	cmd.Flags().StringVarP(&sqlArgs.DBDsn, "db-dsn", "d", "", "数据库连接地址，例如 user:password@(host:port)/database") //nolint
	_ = cmd.MarkFlagRequired("db-dsn")                                                                         // 标记 db-dsn 参数为必填
	cmd.Flags().StringVarP(&dbTables, "db-table", "t", "", "表名，多个表名用逗号分隔")
	_ = cmd.MarkFlagRequired("db-table") // 标记 db-table 参数为必填
	cmd.Flags().BoolVarP(&sqlArgs.IsEmbed, "embed", "e", false, "是否嵌入 gorm.Model 结构体")
	cmd.Flags().IntVarP(&sqlArgs.JSONNamedType, "json-name-type", "j", 1, "JSON 标签名称类型，0: snake case, 1: camel case")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./model_<时间>")

	return cmd
}

// modelGenerator 模型代码生成器
type modelGenerator struct {
	codes   map[string]string // 生成的代码
	outPath string            // 输出目录
}

// generateCode 生成模型代码
func (g *modelGenerator) generateCode() (string, error) {
	subTplName := codeNameModel     // 子模板名称
	r := Replacers[TplNameSunshine] // 获取替换器
	if r == nil {
		return "", errors.New("replacer is nil") // 替换器为空时返回错误
	}

	// 指定子目录和文件
	var subDirs []string
	subFiles := []string{"internal/model/userExample.go"}

	r.SetSubDirsAndFiles(subDirs, subFiles...) // 设置子目录和文件
	fields := g.addFields(r)                   // 添加替换字段
	r.SetReplacementFields(fields)             // 设置替换字段
	_ = r.SetOutputDir(g.outPath, subTplName)  // 设置输出目录
	if err := r.SaveFiles(); err != nil {
		return "", err // 保存文件时返回错误
	}

	return r.GetOutputDir(), nil // 返回输出目录
}

// addFields 添加替换字段
func (g *modelGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	fields = append(fields, deleteFieldsMark(r, modelFile, startMark, endMark)...) // 删除标记字段
	fields = append(fields, []replacer.Field{
		{ // 替换 model/userExample.go 文件的内容
			Old: modelFileMark,
			New: g.codes[parser.CodeTypeModel],
		},
		{
			Old:             "UserExample",
			New:             g.codes[parser.TableName],
			IsCaseSensitive: true,
		},
	}...)

	return fields
}
