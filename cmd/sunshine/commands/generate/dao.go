// Package generate 代码生成器
// 提供基于 SQL 的 DAO、Model、Cache 等代码生成功能
package generate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/replacer"
	"github.com/18721889353/sunshine/pkg/sql2code"
	"github.com/18721889353/sunshine/pkg/sql2code/parser"
)

// DaoCommand generate dao code
func DaoCommand(parentName string) *cobra.Command {
	var (
		moduleName      string // go.mod module name
		outPath         string // output directory
		dbTables        string // table names
		isIncludeInitDB bool

		sqlArgs = sql2code.Args{
			Package:  "model",
			JSONTag:  true,
			GormType: true,
		}

		serverName     string // server name
		suitedMonoRepo bool   // whether the generated code is suitable for mono-repo
	)

	cmd := &cobra.Command{
		Use:   "dao",                                      // 子命令名称：sunshine web/grpc dao
		Short: "基于 SQL 自动生成 DAO 层代码",                      // 简短描述（help 中一行展示）
		Long:  "基于 SQL 表结构自动生成数据访问层（DAO）代码，包含 CRUD 操作方法。", // 详细描述（help 中详细展示）
		Example: color.HiBlackString(fmt.Sprintf(`  # =====================================================================
  # 基本用法：根据数据库表生成 DAO、Model、Cache 代码
  # 执行后会生成: internal/dao/{table}.go（含 CRUD 方法）、internal/model/{table}.go
  # =====================================================================
  sunshine %[1]s dao \
    --module-name=yourModuleName \
    --db-driver=mysql \
    --db-dsn=root:123456@(192.168.3.37:3306)/test \
    --db-table=user \
    --extended-api=true \
    --embed=true \
    --json-name-type=1 \
    --include-init-db=true \
    --server-name=user-service \
    --suited-mono-repo=false \
    --out=/d/Temp/web


  # =====================================================================
  # 参数说明：
  #   --module-name    Go 模块名（必填，除非 out 目录已有 gen.info）
  #   --db-driver      数据库驱动类型（默认 mysql）
  #   --db-dsn         数据库连接地址（必填），格式: user:password@(host:port)/database
  #   --db-table       数据库表名（必填），多表用逗号分隔
  #   --embed          是否嵌入 gorm.Model 结构体（可选，默认 false）
  #   --extended-api   是否生成扩展 CRUD API（可选，默认 false）
  #   --server-name    服务名（mono-repo 模式必填）
  #   --suited-mono-repo 是否适配单体仓库结构（可选，默认 false）
  #   --json-name-type JSON 标签风格，0:下划线, 1:驼峰（可选，默认 1）
  #   --include-init-db 是否包含 MySQL 和 Redis 初始化代码（可选，默认 false）
  #   --out            输出目录（可选，默认 ./dao_<时间戳>）
`, parentName)),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if moduleName == "" {
				return fmt.Errorf(`required flag(s) "module-name" not set, use "sunshine %s dao -h" for help`, parentName)
			}
			if suitedMonoRepo {
				if serverName == "" {
					return fmt.Errorf(`required flag(s) "server-name" not set, use "sunshine %s dao -h" for help`, parentName)
				}
				serverName = convertServerName(serverName)
				outPath = changeOutPath(outPath, serverName)
			}

			tableNames := strings.Split(dbTables, ",")
			for count, tableName := range tableNames {
				if tableName == "" {
					continue
				}
				sqlArgs.DBTable = tableName
				codes, err := sql2code.Generate(&sqlArgs)
				if err != nil {
					return err
				}

				// control to generate the initialization db code only once
				if count == 0 && isIncludeInitDB {
					isIncludeInitDB = true
				} else {
					isIncludeInitDB = false
				}

				var g = &daoGenerator{
					moduleName:      moduleName,
					dbDriver:        sqlArgs.DBDriver,
					isIncludeInitDB: isIncludeInitDB,
					codes:           codes,
					outPath:         outPath,
					serverName:      serverName,
					isEmbed:         sqlArgs.IsEmbed,
					isExtendedAPI:   sqlArgs.IsExtendedAPI,
					suitedMonoRepo:  suitedMonoRepo,
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
			fmt.Printf("generate \"dao\" code successfully, out = %s\n", outPath)
			return nil
		},
	}

	// 注册命令行参数
	// --module-name / -m: Go 模块名称，对应 go.mod 文件中的 module 声明
	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "模块名称，对应 go.mod 文件中的 module 声明")
	//_ = cmd.MarkFlagRequired("module-name")
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
	// --extended-api / -a: 是否生成扩展 CRUD API
	cmd.Flags().BoolVarP(&sqlArgs.IsExtendedAPI, "extended-api", "a", false, "是否生成扩展 CRUD API，额外包含: DeleteByIDs, GetByCondition, ListByIDs, ListByLatestID")
	// --server-name / -s: 服务器名称
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "服务器名称，用于单体仓库模式")
	// --suited-mono-repo / -l: 是否适配单体仓库
	cmd.Flags().BoolVarP(&suitedMonoRepo, "suited-mono-repo", "l", false, "是否适配单体仓库结构")
	// --json-name-type / -j: JSON 标签命名风格
	cmd.Flags().IntVarP(&sqlArgs.JSONNamedType, "json-name-type", "j", 1, "JSON 标签命名风格，0:下划线, 1:驼峰")
	// --out / -o: 输出目录
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "代码输出目录，默认为 ./dao_<时间戳>，"+flagTip("module-name"))
	// --include-init-db / -i: 是否包含初始化代码
	cmd.Flags().BoolVarP(&isIncludeInitDB, "include-init-db", "i", false, "是否包含 MySQL 和 Redis 初始化代码")

	return cmd
}

// daoGenerator DAO 代码生成器
type daoGenerator struct {
	moduleName      string            // 模块名称
	dbDriver        string            // 数据库驱动
	isIncludeInitDB bool              // 是否包含初始化代码
	codes           map[string]string // SQL 解析后的代码映射
	outPath         string            // 输出目录
	isEmbed         bool              // 是否嵌入 gorm.Model
	isExtendedAPI   bool              // 是否生成扩展 API
	serverName      string            // 服务器名称
	suitedMonoRepo  bool              // 是否适配单体仓库

	fields []replacer.Field // 替换字段列表
}

// generateCode 生成 DAO 代码
func (g *daoGenerator) generateCode() (string, error) {
	subTplName := codeNameDao
	r, err := replacer.New(SunshineDir)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// 指定子目录和文件
	var subDirs []string
	subFiles := []string{}

	selectFiles := map[string][]string{
		"internal/cache": {
			"userExample.go", "userExample_test.go",
		},
		"internal/dao": {
			"userExample.go", "userExample_test.go",
		},
		"internal/model": {
			"userExample.go",
		},
	}

	info := g.codes[parser.CodeTypeCrudInfo]
	crudInfo, err := unmarshalCrudInfo(info)
	if err != nil {
		return "", err
	}
	// 使用 userExample.go 作为 Self-Testing Template（可直接编译验证）
	if g.isExtendedAPI {
		selectFiles = map[string][]string{
			"internal/cache": {
				"userExample.go",
			},
			"internal/dao": {
				"userExample.go",
			},
			"internal/model": {
				"userExample.go",
			},
		}
	} else if crudInfo.CheckCommonType() {
		selectFiles = map[string][]string{
			"internal/cache": {
				"userExample.go",
			},
			"internal/dao": {
				"userExample.go",
			},
			"internal/model": {
				"userExample.go",
			},
		}
	}

	replaceFiles := make(map[string][]string)
	switch strings.ToLower(g.dbDriver) {
	case DBDriverMysql:
		g.fields = append(g.fields, getExpectedSQLForDeletionField(g.isEmbed)...)

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

	return r.GetOutputDir(), nil
}

// addFields 添加字段替换规则
func (g *daoGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field
	// 合并预定义的替换字段
	fields = append(fields, g.fields...)
	// 删除模板中的编译占位代码
	fields = append(fields, genDeleteMarkFields(r, modelFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, daoFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, daoTestFile, startMark, endMark)...)
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
		{ // 替换模块导入路径
			Old: selfPackageName + "/" + r.GetSourcePath(),
			New: g.moduleName,
		},
		{ // 替换 sunshine 框架导入路径
			Old: "github.com/18721889353/sunshine",
			New: g.moduleName,
		},
		{ // 恢复 pkg 导入路径
			Old: g.moduleName + pkgPathSuffix,
			New: "github.com/18721889353/sunshine/pkg",
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
