package generate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/replacer"
	"github.com/18721889353/sunshine/pkg/sql2code"
	"github.com/18721889353/sunshine/pkg/sql2code/parser"
)

// ProtobufCommand generate protobuf code
func ProtobufCommand() *cobra.Command {
	var (
		moduleName string // module name for go.mod
		serverName string // server name
		outPath    string // output directory
		dbTables   string // table names

		sqlArgs = sql2code.Args{
			JSONTag: true,
		}
	)

	cmd := &cobra.Command{
		Use:   "protobuf",
		Short: "基于 SQL 生成 Protobuf 代码",
		Long:  "基于 SQL 表结构自动生成 Protobuf 代码。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：根据数据库表生成 Protobuf 代码
  # 执行后会生成: api/xxx/v1/（proto 文件）
  # =====================================================================
  sunshine micro protobuf \
    --module-name=yourModuleName \
    --server-name=yourServerName \
    --db-driver=mysql \
    --db-dsn=root:123456@(192.168.3.37:3306)/test \
    --db-table=user \
    --web-type=true \
    --extended-api=true \
    --json-name-type=1 \
    --out=./yourServerDir


  # =====================================================================
  # 参数说明：
  #   --module-name     Go 模块名（必填），对应 go.mod 中的 module 声明
  #   --server-name     服务名（必填）
  #   --db-driver       数据库驱动类型（默认 mysql）
  #   --db-dsn          数据库连接地址（必填）
  #   --db-table        数据库表名（必填），多表用逗号分隔
  #   --web-type        是否生成包含路由和 Swagger 信息的 proto 文件（可选，默认 false）
  #   --extended-api    是否生成扩展 CRUD API（可选，默认 false）
  #   --json-name-type  JSON 标签风格，0:下划线, 1:驼峰（可选，默认 1）
  #   --out             输出目录（可选，默认 ./protobuf_<时间戳>）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if moduleName == "" {
				return errors.New(`required flag(s) "module-name" not set, use "sunshine micro protobuf -h" for help`)
			}
			if serverName == "" {
				return errors.New(`required flag(s) "server-name" not set, use "sunshine micro protobuf -h" for help`)
			}

			serverName = convertServerName(serverName)

			tableNames := strings.Split(dbTables, ",")
			for _, tableName := range tableNames {
				if tableName == "" {
					continue
				}

				sqlArgs.DBTable = tableName
				codes, err := sql2code.Generate(&sqlArgs)
				if err != nil {
					return err
				}

				var g = &protobufGenerator{
					moduleName: moduleName,
					serverName: serverName,
					codes:      codes,
					outPath:    outPath,
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
			fmt.Printf("generate \"protobuf\" code successfully, out = %s\n", outPath)

			return nil
		},
	}

	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "Go 模块名，对应 go.mod 文件中的 module 声明")
	//_ = cmd.MarkFlagRequired("module-name")
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "服务名称")
	//_ = cmd.MarkFlagRequired("server-name")
	cmd.Flags().StringVarP(&sqlArgs.DBDriver, "db-driver", "k", "mysql", "数据库驱动类型，当前支持 mysql")
	cmd.Flags().StringVarP(&sqlArgs.DBDsn, "db-dsn", "d", "", "数据库连接地址，格式: user:password@(host:port)/database") //nolint
	if err := cmd.MarkFlagRequired("db-dsn"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	cmd.Flags().StringVarP(&dbTables, "db-table", "t", "", "数据库表名，多个表名用逗号分隔")
	if err := cmd.MarkFlagRequired("db-table"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	cmd.Flags().IntVarP(&sqlArgs.JSONNamedType, "json-name-type", "j", 1, "JSON 标签命名风格，0:下划线, 1:驼峰")
	cmd.Flags().BoolVarP(&sqlArgs.IsWebProto, "web-type", "w", false, "是否生成包含路由和 Swagger 信息的 proto 文件")
	cmd.Flags().BoolVarP(&sqlArgs.IsExtendedAPI, "extended-api", "a", false, "是否生成扩展 CRUD API，额外包含: DeleteByIDs, GetByCondition, ListByIDs, ListByLatestID")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./protobuf_<时间戳>，"+flagTip("module-name", "server-name"))

	return cmd
}

type protobufGenerator struct {
	moduleName string
	serverName string
	codes      map[string]string
	outPath    string
}

// generateCode 生成 Protobuf 代码
func (g *protobufGenerator) generateCode() (string, error) {
	subTplName := codeNameProtobuf
	r := Replacers[TplNameSunshine]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	if g.serverName == "" {
		g.serverName = g.moduleName
	}

	// 指定子目录和文件
	var subDirs []string
	subFiles := []string{"api/serverNameExample/v1/userExample.proto"}

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
func (g *protobufGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	// 删除模板中的编译占位代码
	fields = append(fields, genDeleteMarkFields(r, protoFile, startMark, endMark)...)
	// 添加核心字段替换规则
	fields = append(fields, []replacer.Field{
		{ // 替换 v1/userExample.proto 文件的内容
			Old: protoFileMark,
			New: g.codes[parser.CodeTypeProto],
		},
		{
			Old: "github.com/18721889353/sunshine",
			New: g.moduleName,
		},
		{ // 替换 api 目录名
			Old: strings.Join([]string{"api", "serverNameExample", "v1"}, gofile.GetPathDelimiter()),
			New: strings.Join([]string{"api", g.serverName, "v1"}, gofile.GetPathDelimiter()),
		},
		{
			Old: "api/serverNameExample/v1",
			New: fmt.Sprintf("api/%s/v1", g.serverName),
		},
		{ // 注意：protobuf package 不允许包含 "-" 符号
			Old: "api.serverNameExample.v1",
			New: fmt.Sprintf("api.%s.v1", g.serverName),
		},
		{
			Old: "serverNameExample",
			New: g.serverName,
		},
		{
			Old:             "UserExample",
			New:             g.codes[parser.TableName],
			IsCaseSensitive: true,
		},
	}...)

	return fields
}
