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
		Use:   "model",
		Short: "基于 SQL 生成 Model 层代码",
		Long:  "基于 SQL 表结构自动生成数据模型（Model）代码。",
		Example: color.HiBlackString(fmt.Sprintf(`  # =====================================================================
  # 基本用法：根据数据库表生成 Model 代码
  # 执行后会生成: internal/model/{table}.go
  # =====================================================================
  sunshine %[1]s model \
    --db-driver=mysql \
    --db-dsn=root:123456@(192.168.3.37:3306)/test \
    --db-table=user \
    --embed=true \
    --json-name-type=1 \
    --out=/d/Temp/web


  # =====================================================================
  # 参数说明：
  #   --db-driver      数据库驱动类型（默认 mysql）
  #   --db-dsn         数据库连接地址（必填），格式: user:password@(host:port)/database
  #   --db-table       数据库表名（必填），多表用逗号分隔
  #   --embed          是否嵌入 gorm.Model 结构体（可选，默认 false）
  #   --json-name-type JSON 标签风格，0:下划线, 1:驼峰（可选，默认 1）
  #   --out            输出目录（可选，默认 ./model_<时间戳>）
`, parentName)),
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

				var g = &modelGenerator{
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

	// --db-driver / -k: 数据库驱动类型
	cmd.Flags().StringVarP(&sqlArgs.DBDriver, "db-driver", "k", "mysql", "数据库驱动类型，当前支持 mysql")
	// --db-dsn / -d: 数据库连接地址
	cmd.Flags().StringVarP(&sqlArgs.DBDsn, "db-dsn", "d", "", "数据库连接地址，格式: user:password@(host:port)/database") //nolint
	if err := cmd.MarkFlagRequired("db-dsn"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	// --db-table / -t: 数据库表名
	cmd.Flags().StringVarP(&dbTables, "db-table", "t", "", "数据库表名，多个表用逗号分隔")
	if err := cmd.MarkFlagRequired("db-table"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	// --embed / -e: 是否嵌入 gorm.Model
	cmd.Flags().BoolVarP(&sqlArgs.IsEmbed, "embed", "e", false, "是否嵌入 gorm.Model 结构体")
	// --json-name-type / -j: JSON 标签命名风格
	cmd.Flags().IntVarP(&sqlArgs.JSONNamedType, "json-name-type", "j", 1, "JSON 标签命名风格，0:下划线, 1:驼峰")
	// --out / -o: 输出目录
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./model_<时间戳>")

	return cmd
}

// modelGenerator 模型代码生成器
type modelGenerator struct {
	codes   map[string]string // 生成的代码
	outPath string            // 输出目录
}

// generateCode 生成模型代码
func (g *modelGenerator) generateCode() (string, error) {
	subTplName := codeNameModel // 子模板名称
	r := Replacers[TplNameSunshine] // 获取替换器
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// 指定子目录和文件
	var subDirs []string
	subFiles := []string{"internal/model/userExample.go"}

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

// addFields 添加替换字段
func (g *modelGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	// 删除模板中的编译占位代码
	fields = append(fields, genDeleteMarkFields(r, modelFile, startMark, endMark)...)
	// 添加核心字段替换规则
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
