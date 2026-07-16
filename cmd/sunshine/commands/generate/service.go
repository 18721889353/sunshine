package generate

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/replacer"
	"github.com/18721889353/sunshine/pkg/sql2code"
	"github.com/18721889353/sunshine/pkg/sql2code/parser"
)

// ServiceCommand generate service code
func ServiceCommand() *cobra.Command {
	var (
		moduleName string // module name for go.mod
		serverName string // server name
		outPath    string // output directory
		dbTables   string // table names

		sqlArgs = sql2code.Args{
			Package:  "model",
			JSONTag:  true,
			GormType: true,
		}

		suitedMonoRepo bool // whether the generated code is suitable for mono-repo
	)

	cmd := &cobra.Command{
		Use:   "service",
		Short: "基于 SQL 生成 gRPC Service CRUD 代码",
		Long:  "基于 SQL 表结构自动生成 gRPC Service CRUD 代码，包含缓存、数据访问、错误码等。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：根据数据库表生成 gRPC Service CRUD 代码
  # 执行后会生成: internal/service/、internal/dao/、internal/cache/、api/xxx/v1/ 等
  # =====================================================================
  sunshine micro service \
    --module-name=yourModuleName \
    --server-name=yourServerName \
    --db-driver=mysql \
    --db-dsn=root:123456@(192.168.3.37:3306)/test \
    --db-table=user \
    --embed=true \
    --extended-api=true \
    --json-name-type=1 \
    --suited-mono-repo=false \
    --out=./yourServerDir


  # =====================================================================
  # 参数说明：
  #   --module-name     Go 模块名（必填），对应 go.mod 中的 module 声明
  #   --server-name     服务名（必填）
  #   --db-driver       数据库驱动类型（默认 mysql）
  #   --db-dsn          数据库连接地址（必填）
  #   --db-table        数据库表名（必填），多表用逗号分隔
  #   --embed           是否嵌入 gorm.Model 结构体（可选，默认 false）
  #   --extended-api    是否生成扩展 CRUD API（可选，默认 false）
  #   --suited-mono-repo 是否适配单体仓库结构（可选，默认 false）
  #   --json-name-type  JSON 标签风格，0:下划线, 1:驼峰（可选，默认 1）
  #   --out             输出目录（可选，默认 ./service_<时间戳>）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if moduleName == "" {
				return errors.New(`required flag(s) "module-name" not set, use "sunshine micro service -h" for help`)
			}
			if serverName == "" {
				return errors.New(`required flag(s) "server-name" not set, use "sunshine micro service -h" for help`)
			}

			serverName = convertServerName(serverName)
			if suitedMonoRepo {
				outPath = changeOutPath(outPath, serverName)
			}

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

				var g = &serviceGenerator{
					moduleName:     moduleName,
					serverName:     serverName,
					dbDriver:       sqlArgs.DBDriver,
					isEmbed:        sqlArgs.IsEmbed,
					isExtendedAPI:  sqlArgs.IsExtendedAPI,
					codes:          codes,
					outPath:        outPath,
					suitedMonoRepo: suitedMonoRepo,
				}
				outPath, err = g.generateCode()
				if err != nil {
					return err
				}
			}

			fmt.Printf(`
using help:
  1. move the folders "api" and "internal" to your project code folder.
  2. open a terminal and execute the command to generate code: make proto
  3. compile and run service: make run
  4. open the file internal/service/xxx_client_test.go using Goland or VS Code, and test the grpc CRUD api.

`)
			fmt.Printf("generate \"service\" code successfully, out = %s\n", outPath)
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
	cmd.Flags().BoolVarP(&sqlArgs.IsEmbed, "embed", "e", false, "是否嵌入 gorm.Model 结构体")
	cmd.Flags().BoolVarP(&sqlArgs.IsExtendedAPI, "extended-api", "a", false, "是否生成扩展 CRUD API，额外包含: DeleteByIDs, GetByCondition, ListByIDs, ListByLatestID")
	cmd.Flags().BoolVarP(&suitedMonoRepo, "suited-mono-repo", "l", false, "是否适配单体仓库结构")
	cmd.Flags().IntVarP(&sqlArgs.JSONNamedType, "json-name-type", "j", 1, "JSON 标签命名风格，0:下划线, 1:驼峰")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./service_<时间戳>，"+flagTip("module-name", "server-name"))

	return cmd
}

type serviceGenerator struct {
	moduleName     string
	serverName     string
	dbDriver       string
	isEmbed        bool
	isExtendedAPI  bool
	codes          map[string]string
	outPath        string
	suitedMonoRepo bool

	fields        []replacer.Field
	isCommonStyle bool
}

// generateCode 生成 Service 代码
func (g *serviceGenerator) generateCode() (string, error) {
	subTplName := codeNameService
	r, err := replacer.New(SunshineDir)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	if g.serverName == "" {
		g.serverName = g.moduleName
	}

	// 指定子目录和文件
	var subDirs []string
	var subFiles []string

	selectFiles := map[string][]string{
		"api/serverNameExample/v1": {
			"userExample.proto",
		},
		"internal/cache": {
			"userExample.go", "userExample_test.go",
		},
		"internal/dao": {
			"userExample.go", "userExample_test.go",
		},
		"internal/ecode": {
			"userExample_rpc.go",
		},
		"internal/model": {
			"userExample.go",
		},
		"internal/service": {
			"userExample.go", "userExample_client_test.go",
		},
	}

	info := g.codes[parser.CodeTypeCrudInfo]
	crudInfo, err := unmarshalCrudInfo(info)
	if err != nil {
		return "", err
	}
	if crudInfo.CheckCommonType() {
		g.isCommonStyle = true
		selectFiles = map[string][]string{
			"api/serverNameExample/v1": {
				"userExample.proto",
			},
			"internal/cache": {
				"userExample.go",
			},
			"internal/dao": {
				"userExample.go",
			},
			"internal/ecode": {
				"userExample_rpc.go",
			},
			"internal/model": {
				"userExample.go",
			},
			"internal/service": {
				"userExample.go",
			},
		}
		var fields []replacer.Field
		if g.isExtendedAPI {
			selectFiles["internal/dao"] = []string{"userExample.go"}
			selectFiles["internal/ecode"] = []string{"userExample_rpc.go"}
			selectFiles["internal/service"] = []string{"userExample.go"}
			fields = commonServiceExtendedFields(r)
		} else {
			fields = commonServiceFields(r)
		}
		g.fields = append(g.fields, fields...)
	}

	replaceFiles := make(map[string][]string)
	switch strings.ToLower(g.dbDriver) {
	case DBDriverMysql:
		g.fields = append(g.fields, getExpectedSQLForDeletionField(g.isEmbed)...)
		if g.isExtendedAPI {
			var fields []replacer.Field
			if !crudInfo.CheckCommonType() {
				replaceFiles, fields = serviceExtendedAPI(r, codeNameService)
			}
			g.fields = append(g.fields, fields...)
		}

	default:
		return "", dbDriverErr(g.dbDriver)
	}

	subFiles = append(subFiles, getSubFiles(selectFiles, replaceFiles)...)

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

	if g.suitedMonoRepo {
		if err := moveProtoFileToAPIDir(g.moduleName, g.serverName, g.suitedMonoRepo, r.GetOutputDir()); err != nil {
			return "", err
		}
	}

	return r.GetOutputDir(), nil
}

// addFields 添加字段替换规则
func (g *serviceGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field
	// 合并预定义的替换字段
	fields = append(fields, g.fields...)
	// 删除模板中的编译占位代码
	fields = append(fields, genDeleteMarkFields(r, modelFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, daoFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, daoTestFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, serviceLogicFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, protoFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, serviceClientFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, serviceTestFile, startMark, endMark)...)
	// 添加核心字段替换规则
	fields = append(fields, []replacer.Field{
		{ // 替换 model/userExample.go 文件的内容
			Old: modelFileMark,
			New: g.codes[parser.CodeTypeModel],
		},
		{ // 替换 dao/userExample.go 文件的内容
			Old: daoFileMark,
			New: g.codes[parser.CodeTypeDAO],
		},
		{ // 替换 handler/userExample_logic.go 文件的内容
			Old: embedTimeMark,
			New: getEmbedTimeCode(g.isEmbed),
		},
		{ // 替换 v1/userExample.proto 文件的内容
			Old: protoFileMark,
			New: g.codes[parser.CodeTypeProto],
		},
		{ // 替换 service/userExample_client_test.go 文件的内容
			Old: serviceFileMark,
			New: adjustmentOfIDType(g.codes[parser.CodeTypeService], g.dbDriver, g.isCommonStyle),
		},
		{ // 替换模块导入路径
			Old: selfPackageName + "/" + r.GetSourcePath(),
			New: g.moduleName,
		},
		{ // 替换 sunshine 框架导入路径
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
		{ // 替换示例编号
			Old: "_userExampleNO       = 2",
			New: fmt.Sprintf("_userExampleNO       = %d", rand.Intn(99)+1),
		},
		{ // 恢复 pkg 导入路径
			Old: g.moduleName + pkgPathSuffix,
			New: "github.com/18721889353/sunshine/pkg",
		},
		{ // 替换服务名
			Old: "serverNameExample",
			New: g.serverName,
		},
		{ // 替换数据库显示名称
			Old: showDbNameMark,
			New: CurrentDbDriver(g.dbDriver),
		},
		{ // 替换表名
			Old:             "UserExample",
			New:             g.codes[parser.TableName],
			IsCaseSensitive: true,
		},
	}...)

	// 单体仓库适配
	if g.suitedMonoRepo {
		fs := SubServerCodeFields(g.moduleName, g.serverName)
		fields = append(fields, fs...)
	}

	return fields
}

func serviceExtendedAPI(r replacer.Replacer, codeName string) (map[string][]string, []replacer.Field) {
	replaceFiles := map[string][]string{
		"internal/ecode": {
			"systemCode_rpc.go", "userExample_rpc.go",
		},
		"internal/service": {
			"service.go", "service_test.go", "userExample.go", "userExample_client_test.go",
		},
	}
	if codeName == codeNameService {
		replaceFiles["internal/ecode"] = []string{"userExample_rpc.go"}
		replaceFiles["internal/service"] = []string{"userExample.go", "userExample_client_test.go"}
	}

	var fields []replacer.Field

	return replaceFiles, fields
}

func commonServiceFields(r replacer.Replacer) []replacer.Field {
	return nil
}

func commonServiceExtendedFields(r replacer.Replacer) []replacer.Field {
	return nil
}
