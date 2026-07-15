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
		Short: "Generate grpc service CRUD code based on sql",
		Long:  "Generate grpc service CRUD code based on sql.",
		Example: color.HiBlackString(`  # Generate service code.
  sunshine micro service --module-name=yourModuleName --server-name=yourServerName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user

  # Generate service code with multiple table names.
  sunshine micro service --module-name=yourModuleName --server-name=yourServerName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=t1,t2

  # Generate service code with extended api.
  sunshine micro service --module-name=yourModuleName --server-name=yourServerName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --extended-api=true

  # Generate service code and specify the server directory, Note: code generation will be canceled when the latest generated file already exists.
  sunshine micro service --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --out=./yourServerDir

  # If you want the generated code to suited to mono-repo, you need to set the parameter --suited-mono-repo=true`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			mdName, srvName, smr := getNamesFromOutDir(outPath)
			if mdName != "" {
				moduleName = mdName
				suitedMonoRepo = smr
			} else if moduleName == "" {
				return errors.New(`required flag(s) "module-name" not set, use "sunshine micro service -h" for help`)
			}
			if srvName != "" {
				serverName = srvName
			} else if serverName == "" {
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

				g := &serviceGenerator{
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

	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "module-name is the name of the module in the go.mod file")
	//_ = cmd.MarkFlagRequired("module-name")
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "server name")
	//_ = cmd.MarkFlagRequired("server-name")
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
	cmd.Flags().BoolVarP(&suitedMonoRepo, "suited-mono-repo", "l", false, "whether the generated code is suitable for mono-repo")
	cmd.Flags().IntVarP(&sqlArgs.JSONNamedType, "json-name-type", "j", 1, "json tags name type, 0:snake case, 1:camel case")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output directory, default is ./service_<time>, "+flagTip("module-name", "server-name"))

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

// nolint
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

	// setting up template information
	subDirs := []string{}
	subFiles := []string{}

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
	if err := r.SetOutputDir(g.outPath, subTplName); err != nil {
		return "", err
	}
	fields := g.addFields(r)
	r.SetReplacementFields(fields)
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

func (g *serviceGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field
	fields = append(fields, g.fields...)
	fields = append(fields, deleteFieldsMark(r, modelFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoTestFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, serviceLogicFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, protoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, serviceClientFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, serviceTestFile, startMark, endMark)...)
	fields = append(fields, []replacer.Field{
		{ // replace the contents of the model/userExample.go file
			Old: modelFileMark,
			New: g.codes[parser.CodeTypeModel],
		},
		{ // replace the contents of the dao/userExample.go file
			Old: daoFileMark,
			New: g.codes[parser.CodeTypeDAO],
		},
		{ // replace the contents of the handler/userExample_logic.go file
			Old: embedTimeMark,
			New: getEmbedTimeCode(g.isEmbed),
		},
		{ // replace the contents of the v1/userExample.proto file
			Old: protoFileMark,
			New: g.codes[parser.CodeTypeProto],
		},
		{ // replace the contents of the service/userExample_client_test.go file
			Old: serviceFileMark,
			New: adjustmentOfIDType(g.codes[parser.CodeTypeService], g.dbDriver, g.isCommonStyle),
		},
		{
			Old: selfPackageName + "/" + r.GetSourcePath(),
			New: g.moduleName,
		},
		{
			Old: "github.com/18721889353/sunshine",
			New: g.moduleName,
		},
		// replace directory name
		{
			Old: strings.Join([]string{"api", "serverNameExample", "v1"}, gofile.GetPathDelimiter()),
			New: strings.Join([]string{"api", g.serverName, "v1"}, gofile.GetPathDelimiter()),
		},
		{
			Old: "api/serverNameExample/v1",
			New: fmt.Sprintf("api/%s/v1", g.serverName),
		},
		// Note: protobuf package no "-" signs allowed
		{
			Old: "api.serverNameExample.v1",
			New: fmt.Sprintf("api.%s.v1", g.serverName),
		},
		{
			Old: "_userExampleNO       = 2",
			New: fmt.Sprintf("_userExampleNO       = %d", rand.Intn(99)+1),
		},
		{
			Old: g.moduleName + pkgPathSuffix,
			New: "github.com/18721889353/sunshine/pkg",
		},
		{
			Old: "serverNameExample",
			New: g.serverName,
		},
		{
			Old: showDbNameMark,
			New: CurrentDbDriver(g.dbDriver),
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
