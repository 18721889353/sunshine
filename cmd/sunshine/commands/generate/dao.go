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
		Use:   "dao",
		Short: "Generate dao code based on sql",
		Long:  "Generate dao code based on sql.",
		Example: color.HiBlackString(fmt.Sprintf(`  # Generate dao code.
  sunshine %s dao --module-name=yourModuleName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user

  # Generate dao code with multiple table names.
  sunshine %s dao --module-name=yourModuleName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=t1,t2

  # Generate dao code with extened api.
  sunshine %s dao --module-name=yourModuleName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --extended-api=true

  # Generate dao code and specify the server directory, Note: code generation will be canceled when the latest generated file already exists.
  sunshine %s dao --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --out=./yourServerDir

  # If you want the generated code to suited to mono-repo, you need to set the parameter --suited-mono-repo=true --server-name=yourServerName`,
			parentName, parentName, parentName, parentName)),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			mdName, srvName, smr := getNamesFromOutDir(outPath)
			if mdName != "" {
				moduleName = mdName
				serverName = srvName
				suitedMonoRepo = smr
			} else if moduleName == "" {
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

				g := &daoGenerator{
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

	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "module-name is the name of the module in the go.mod file")
	//_ = cmd.MarkFlagRequired("module-name")
	cmd.Flags().StringVarP(&sqlArgs.DBDriver, "db-driver", "k", "mysql", "database driver, support mysql")
	cmd.Flags().StringVarP(&sqlArgs.DBDsn, "db-dsn", "d", "", "database content address, e.g. user:password@(host:port)/database") //nolint
	if err := cmd.MarkFlagRequired("db-dsn"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(&dbTables, "db-table", "t", "", "table name, multiple names separated by commas")
	if err := cmd.MarkFlagRequired("db-table"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().BoolVarP(&sqlArgs.IsEmbed, "embed", "e", false, "whether to embed gorm.model struct")
	cmd.Flags().BoolVarP(&sqlArgs.IsExtendedAPI, "extended-api", "a", false, "whether to generate extended crud api, additional includes: DeleteByIDs, GetByCondition, ListByIDs, ListByLatestID")
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "server name")
	cmd.Flags().BoolVarP(&suitedMonoRepo, "suited-mono-repo", "l", false, "whether the generated code is suitable for mono-repo")
	cmd.Flags().IntVarP(&sqlArgs.JSONNamedType, "json-name-type", "j", 1, "json tags name type, 0:snake case, 1:camel case")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output directory, default is ./dao_<time>, "+flagTip("module-name"))
	cmd.Flags().BoolVarP(&isIncludeInitDB, "include-init-db", "i", false, "if true, includes mysql and redis initialization code")

	return cmd
}

type daoGenerator struct {
	moduleName      string
	dbDriver        string
	isIncludeInitDB bool
	codes           map[string]string
	outPath         string
	isEmbed         bool
	isExtendedAPI   bool
	serverName      string
	suitedMonoRepo  bool

	fields []replacer.Field
}

func (g *daoGenerator) generateCode() (string, error) {
	subTplName := codeNameDao
	r, err := replacer.New(SunshineDir)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", errors.New("r is nil")
	}

	// specify the subdirectory and files
	subDirs := []string{}
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

// set fields
func (g *daoGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field
	fields = append(fields, g.fields...)
	fields = append(fields, deleteFieldsMark(r, modelFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoTestFile, startMark, endMark)...)
	fields = append(fields, []replacer.Field{
		{ // replace the contents of the model/userExample.go file
			Old: modelFileMark,
			New: g.codes[parser.CodeTypeModel],
		},
		{
			Old: daoFileMark,
			New: g.codes[parser.CodeTypeDAO],
		},
		{
			Old: selfPackageName + "/" + r.GetSourcePath(),
			New: g.moduleName,
		},
		{
			Old: "github.com/18721889353/sunshine",
			New: g.moduleName,
		},
		{
			Old: g.moduleName + pkgPathSuffix,
			New: "github.com/18721889353/sunshine/pkg",
		},
		{
			Old:             "UserExample",
			New:             g.codes[parser.TableName],
			IsCaseSensitive: true,
		},
	}...)

	if g.suitedMonoRepo {
		fs := SubServerCodeFields(g.moduleName, g.serverName)
		fields = append(fields, fs...)
	}

	return fields
}
