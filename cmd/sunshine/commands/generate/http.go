package generate

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"

	"github.com/fatih/color"
	"github.com/huandu/xstrings"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/replacer"
	"github.com/18721889353/sunshine/pkg/sql2code"
	"github.com/18721889353/sunshine/pkg/sql2code/parser"
)

// HTTPCommand generate web service code
func HTTPCommand() *cobra.Command {
	var (
		moduleName  string // module name for go.mod
		serverName  string // server name
		projectName string // project name for deployment name
		repoAddr    string // image repo address
		outPath     string // output directory
		dbTables    string // table names
		sqlArgs     = sql2code.Args{
			Package:  "model",
			JSONTag:  true,
			GormType: true,
		}

		suitedMonoRepo bool // whether the generated code is suitable for mono-repo
	)

	//nolint
	cmd := &cobra.Command{
		Use:   "http",
		Short: "基于 SQL 生成 Web 服务代码",
		Long:  "基于 SQL 表结构自动生成完整的 Web 服务代码，包含 handler、dao、cache、model、router 等。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：根据数据库表生成完整的 Web 服务代码
  # 执行后会生成: internal/handler/、internal/dao/、internal/cache/、internal/model/ 等
  # =====================================================================
  sunshine web http \
    --module-name=yourModuleName \
    --server-name=yourServerName \
    --project-name=yourProjectName \
    --db-driver=mysql \
    --db-dsn=root:123456@(192.168.3.37:3306)/test \
    --db-table=user \
    --embed=true \
    --extended-api=true \
    --json-name-type=1 \
    --repo-addr=192.168.3.37:9443/user-name \
    --suited-mono-repo=false \
    --out=./yourServerDir


  # =====================================================================
  # 参数说明：
  #   --module-name     Go 模块名（必填），对应 go.mod 中的 module 声明
  #   --server-name     服务名（必填）
  #   --project-name    项目名（必填），用于部署名称
  #   --db-driver       数据库驱动类型（默认 mysql）
  #   --db-dsn          数据库连接地址（必填）
  #   --db-table        数据库表名（必填），多表用逗号分隔
  #   --embed           是否嵌入 gorm.Model 结构体（可选，默认 false）
  #   --extended-api    是否生成扩展 CRUD API（可选，默认 false）
  #   --suited-mono-repo 是否适配单体仓库结构（可选，默认 false）
  #   --json-name-type  JSON 标签风格，0:下划线, 1:驼峰（可选，默认 1）
  #   --repo-addr       Docker 镜像仓库地址（可选），不含 http 和仓库名
  #   --out             输出目录（可选，默认 ./serverName_http_<时间戳>）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			var err error
			var firstTable string
			var handlerTableNames []string
			tableNames := strings.Split(dbTables, ",")
			if len(tableNames) == 1 {
				firstTable = tableNames[0]
			} else if len(tableNames) > 1 {
				firstTable = tableNames[0]
				handlerTableNames = tableNames[1:]
			}

			projectName, serverName, err = convertProjectAndServerName(projectName, serverName)
			if err != nil {
				return err
			}

			if suitedMonoRepo {
				outPath = changeOutPath(outPath, serverName)
			}

			sqlArgs.DBTable = firstTable
			codes, err := sql2code.Generate(&sqlArgs)
			if err != nil {
				return err
			}
			var g = &httpGenerator{
				moduleName:     moduleName,
				serverName:     serverName,
				projectName:    projectName,
				repoAddr:       repoAddr,
				dbDSN:          sqlArgs.DBDsn,
				dbDriver:       sqlArgs.DBDriver,
				codes:          codes,
				outPath:        outPath,
				isExtendedAPI:  sqlArgs.IsExtendedAPI,
				isEmbed:        sqlArgs.IsEmbed,
				suitedMonoRepo: suitedMonoRepo,
			}
			outPath, err = g.generateCode()
			if err != nil {
				return err
			}

			for _, handlerTableName := range handlerTableNames {
				if handlerTableName == "" {
					continue
				}

				sqlArgs.DBTable = handlerTableName
				codes, err := sql2code.Generate(&sqlArgs)
				if err != nil {
					return err
				}

				var hg = &handlerGenerator{
					moduleName:    moduleName,
					dbDriver:      sqlArgs.DBDriver,
					codes:         codes,
					outPath:       outPath,
					isEmbed:       sqlArgs.IsEmbed,
					isExtendedAPI: sqlArgs.IsExtendedAPI,
					serverName:    serverName,

					suitedMonoRepo: suitedMonoRepo,
				}
				outPath, err = hg.generateCode()
				if err != nil {
					return err
				}
			}

			fmt.Printf(`
using help:
  1. open a terminal and execute the command to generate the swagger documentation: make docs
  2. compile and run service: make run
  3. visit http://localhost:8080/swagger/index.html in your browser, and test the http CRUD api.

`)
			fmt.Printf("generate %s's web service code successfully, out = %s\n", serverName, outPath)

			_ = generateConfigmap(serverName, outPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "Go 模块名，对应 go.mod 文件中的 module 声明")
	if err := cmd.MarkFlagRequired("module-name"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "服务名称")
	if err := cmd.MarkFlagRequired("server-name"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	cmd.Flags().StringVarP(&projectName, "project-name", "p", "", "项目名称，用于部署名称")
	if err := cmd.MarkFlagRequired("project-name"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
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
	cmd.Flags().StringVarP(&repoAddr, "repo-addr", "r", "", "Docker 镜像仓库地址，不含 http 和仓库名")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./serverName_http_<时间戳>，如果 suited-mono-repo=true 则输出到服务名目录")

	return cmd
}

type httpGenerator struct {
	moduleName     string
	serverName     string
	projectName    string
	repoAddr       string
	dbDSN          string
	dbDriver       string
	codes          map[string]string
	outPath        string
	isEmbed        bool
	isExtendedAPI  bool
	suitedMonoRepo bool

	fields        []replacer.Field
	isCommonStyle bool
}

func (g *httpGenerator) generateCode() (string, error) {
	subTplName := codeNameHTTP
	r, err := replacer.New(SunshineDir)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// specify the subdirectory and files
	subDirs := []string{
		"cmd/serverNameExample_httpExample", "sunshine/configs", "sunshine/deployments", "sunshine/scripts",
	}
	subFiles := []string{
		"sunshine/.gitignore", "sunshine/.golangci.yml", "sunshine/go.mod", "sunshine/go.sum",
		"sunshine/Jenkinsfile", "sunshine/Makefile-for-http", "sunshine/README.md",
	}
	if g.suitedMonoRepo {
		subFiles = removeElements(subFiles, "sunshine/go.mod", "sunshine/go.sum")
	}

	selectFiles := map[string][]string{
		"docs": {
			"docs.go",
		},
		"internal/cache": {
			"userExample.go", "userExample_test.go",
		},
		"internal/config": {
			"serverNameExample.go",
		},
		"internal/dao": {
			"userExample.go", "userExample_test.go",
		},
		"internal/database": {
			"init.go",
		},
		"internal/ecode": {
			"systemCode_http.go", "userExample_http.go",
		},
		"internal/handler": {
			"userExample.go", "userExample_test.go",
		},
		"internal/model": {
			"userExample.go",
		},
		"internal/routers": {
			"routers.go", "userExample.go",
		},
		"internal/server": {
			"http.go", "http_option.go", "cron.go", "cron_option.go", "rabbitmqConsumer.go", "rabbitmqConsumer_option.go",
		},
		"internal/types": {
			"swagger_types.go", "userExample_types.go",
		},
		"internal/cron": {
			"cron.go", "tasks/userExampleCronTask.go",
		},
		"internal/mq": {
			"rabbitmq/mq.go", "rabbitmq/consumers/doingOrder.go",
		},
	}

	if setErr := SetSelectFiles(g.dbDriver, selectFiles); setErr != nil {
		return "", setErr
	}

	info := g.codes[parser.CodeTypeCrudInfo]
	crudInfo, err := unmarshalCrudInfo(info)
	if err != nil {
		return "", err
	}
	if crudInfo.CheckCommonType() {
		g.isCommonStyle = true
		selectFiles["internal/cache"] = []string{"userExample.go"}
		selectFiles["internal/dao"] = []string{"userExample.go"}
		selectFiles["internal/ecode"] = []string{"systemCode_http.go", "userExample_http.go"}
		selectFiles["internal/handler"] = []string{"userExample.go"}
		selectFiles["internal/routers"] = []string{"routers.go", "userExample.go"}
		selectFiles["internal/types"] = []string{"swagger_types.go", "userExample_types.go"}
	}

	replaceFiles := make(map[string][]string)
	switch strings.ToLower(g.dbDriver) {
	case DBDriverMysql:
		g.fields = append(g.fields, getExpectedSQLForDeletionField(g.isEmbed)...)
		if g.isExtendedAPI {
			var fields []replacer.Field
			if !crudInfo.CheckCommonType() {
				replaceFiles, fields = handlerExtendedAPI(r, codeNameHTTP)
			}
			g.fields = append(g.fields, fields...)
		}

	default:
		return "", dbDriverErr(g.dbDriver)
	}

	subFiles = append(subFiles, getSubFiles(selectFiles, replaceFiles)...)

	// ignore some directories and files
	ignoreDirs := []string{"cmd/sunshine"}
	ignoreFiles := []string{"scripts/image-rpc-test.sh", "scripts/patch.sh", "scripts/protoc.sh",
		"scripts/proto-doc.sh"}

	r.SetSubDirsAndFiles(subDirs, subFiles...)
	r.SetIgnoreSubDirs(ignoreDirs...)
	r.SetIgnoreSubFiles(ignoreFiles...)
	if setErr := r.SetOutputDir(g.outPath, g.serverName+"_"+subTplName); setErr != nil {
		return "", setErr
	}
	fields := g.addFields(r)
	r.SetReplacementFields(fields)
	if saveErr := r.SaveFiles(); saveErr != nil {
		return "", saveErr
	}

	// Add replace directive to go.mod for local development
	if !g.suitedMonoRepo {
		if err = appendReplaceDirective(r.GetOutputDir(), g.moduleName); err != nil {
			return "", err
		}
	}

	if saveErr := saveGenInfo(g.moduleName, g.serverName, g.suitedMonoRepo, r.GetOutputDir()); saveErr != nil {
		fmt.Printf("save gen info error: %v\n", saveErr)
	}

	return r.GetOutputDir(), nil
}

func (g *httpGenerator) addFields(r replacer.Replacer) []replacer.Field {
	repoHost, _ := parseImageRepoAddr(g.repoAddr)

	var fields []replacer.Field
	fields = append(fields, g.fields...)
	fields = append(fields, genDeleteMarkFields(r, modelFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, databaseInitDBFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, daoFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, daoTestFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, typesFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, handlerTestFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, httpFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, dockerFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, dockerFileBuild, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, dockerComposeFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, k8sDeploymentFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, k8sServiceFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, imageBuildFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, imageBuildLocalFile, wellStartMark, wellEndMark)...)
	//fields = append(fields, genDeleteAllMarkFields(r, makeFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, gitIgnoreFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteAllMarkFields(r, protoShellFile, wellStartMark, wellEndMark)...)

	//fields = append(fields, genDeleteMarkFields(r, deploymentConfigFile, wellStartMark, wellEndMark)...)
	fields = append(fields, replaceFileContentMark(r, readmeFile,
		setReadmeTitle(g.moduleName, g.serverName, codeNameHTTP, g.suitedMonoRepo))...)
	fields = append(fields, []replacer.Field{
		{ // replace the configuration of the *.yml file
			Old: appConfigFileMark,
			New: httpServerConfigCode,
		},
		{ // replace the configuration of the *.yml file
			Old: appConfigFileMark2,
			New: getDBConfigCode(g.dbDriver),
		},
		{ // replace the contents of the model/userExample.go file
			Old: modelFileMark,
			New: g.codes[parser.CodeTypeModel],
		},
		{ // replace the contents of the database/init.go file
			Old: databaseInitDBFileMark,
			New: getInitDBCode(g.dbDriver),
		},
		{ // replace the contents of the dao/userExample.go file
			Old: daoFileMark,
			New: g.codes[parser.CodeTypeDAO],
		},
		{ // replace the contents of the handler/userExample.go file
			Old: handlerFileMark,
			New: adjustmentOfIDType(g.codes[parser.CodeTypeHandler], g.dbDriver, g.isCommonStyle),
		},
		{ // replace the contents of the Dockerfile file
			Old: dockerFileMark,
			New: dockerFileHTTPCode,
		},
		{ // replace the contents of the Dockerfile_build file
			Old: dockerFileBuildMark,
			New: dockerFileBuildHTTPCode,
		},
		{ // replace the contents of the image-build.sh file
			Old: imageBuildFileMark,
			New: imageBuildFileHTTPCode,
		},
		{ // replace the contents of the image-build-local.sh file
			Old: imageBuildLocalFileMark,
			New: imageBuildLocalFileHTTPCode,
		},
		{ // replace the contents of the docker-compose.yml file
			Old: dockerComposeFileMark,
			New: dockerComposeFileHTTPCode,
		},
		//{ // replace the contents of the *-configmap.yml file
		//	Old: deploymentConfigFileMark,
		//	New: getDBConfigCode(g.dbDriver, true),
		//},
		{ // replace the contents of the *-deployment.yml file
			Old: k8sDeploymentFileMark,
			New: k8sDeploymentFileHTTPCode,
		},
		{ // replace the contents of the *-svc.yml file
			Old: k8sServiceFileMark,
			New: k8sServiceFileHTTPCode,
		},
		{ // replace github.com/18721889353/sunshine/templates/sunshine
			Old: selfPackageName + "/" + r.GetSourcePath(),
			New: g.moduleName,
		},
		{
			Old: protoShellFileGRPCMark,
			New: "",
		},
		{
			Old: protoShellFileMark,
			New: "",
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
			Old: "sunshine api docs",
			New: g.serverName + apiDocsSuffix,
		},
		{
			Old: defaultGoModVersion,
			New: getLocalGoVersion(),
		},
		{
			Old: "userExampleNO       = 1",
			New: fmt.Sprintf("userExampleNO = %d", rand.Intn(99)+1),
		},
		{
			Old: "serverNameExample",
			New: g.serverName,
		},
		// docker image and k8s deployment script replacement
		{
			Old: "server-name-example",
			New: xstrings.ToKebabCase(g.serverName), // snake_case to kebab_case
		},
		// docker image and k8s deployment script replacement
		{
			Old: "project-name-example",
			New: g.projectName,
		},
		{
			Old: "projectNameExample",
			New: g.projectName,
		},
		{
			Old: "repo-addr-example",
			New: g.repoAddr,
		},
		{
			Old: "image-repo-host",
			New: repoHost,
		},
		{
			Old: "_httpExample",
			New: "",
		},
		{
			Old: "_mixExample",
			New: "",
		},
		{
			Old: "_pbExample",
			New: "",
		},
		{
			Old: "root:123456@(192.168.3.37:3306)/account",
			New: g.dbDSN,
		},
		{
			Old: "root:123456@192.168.3.37:27017/account",
			New: g.dbDSN,
		},
		{
			Old: showDbNameMark,
			New: CurrentDbDriver(g.dbDriver),
		},
		{
			Old: "Makefile-for-http",
			New: "Makefile",
		},
		{
			Old:             "UserExample",
			New:             g.codes[parser.TableName],
			IsCaseSensitive: true,
		},
	}...)

	if g.suitedMonoRepo {
		fs := serverCodeFields(codeNameHTTP, g.moduleName, g.serverName)
		fields = append(fields, fs...)
	}

	return fields
}
