package generate

import (
	"errors"
	"fmt"

	"github.com/fatih/color"
	"github.com/huandu/xstrings"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// HTTPPbCommand generate web service code based on protobuf file
func HTTPPbCommand() *cobra.Command {
	var (
		moduleName   string // module name for go.mod
		serverName   string // server name
		projectName  string // project name for deployment name
		repoAddr     string // image repo address
		outPath      string // output directory
		protobufFile string // protobuf file, support * matching

		suitedMonoRepo bool // whether the generated code is suitable for mono-repo
	)

	cmd := &cobra.Command{
		Use:   "http-pb",
		Short: "根据 protobuf 文件生成 Web 服务代码",
		Long:  "根据 protobuf 文件生成 Web 服务代码。",
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：根据 protobuf 文件生成 Web 服务代码
  # 执行后会生成: internal/server/、internal/routers/、internal/ecode/ 等
  # =====================================================================
  sunshine web http-pb \
    --module-name=yourModuleName \
    --server-name=yourServerName \
    --project-name=yourProjectName \
    --protobuf-file=./test.proto \
    --repo-addr=192.168.3.37:9443/user-name \
    --suited-mono-repo=false \
    --out=./yourServerDir


  # =====================================================================
  # 参数说明：
  #   --module-name     Go 模块名（必填），对应 go.mod 中的 module 声明
  #   --server-name     服务名（必填）
  #   --project-name    项目名（必填），用于部署名称
  #   --protobuf-file   proto 文件路径（必填），支持 * 通配符
  #   --suited-mono-repo 是否适配单体仓库结构（可选，默认 false）
  #   --repo-addr       Docker 镜像仓库地址（可选），不含 http 和仓库名
  #   --out             输出目录（可选，默认 ./serverName_http-pb_<时间戳>）
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			var err error
			projectName, serverName, err = convertProjectAndServerName(projectName, serverName)
			if err != nil {
				return err
			}

			if suitedMonoRepo {
				outPath = changeOutPath(outPath, serverName)
			}

			var g = &httpPbGenerator{
				moduleName:   moduleName,
				serverName:   serverName,
				projectName:  projectName,
				protobufFile: protobufFile,
				repoAddr:     repoAddr,
				outPath:      outPath,

				suitedMonoRepo: suitedMonoRepo,
			}
			outPath, err = g.generateCode()
			if err != nil {
				return err
			}

			if configmapErr := generateConfigmap(serverName, outPath); configmapErr != nil {
				fmt.Printf("generate configmap error: %v\n", configmapErr)
			}
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
	cmd.Flags().StringVarP(&protobufFile, "protobuf-file", "f", "", "proto 文件路径，支持 * 通配符")
	if err := cmd.MarkFlagRequired("protobuf-file"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	cmd.Flags().BoolVarP(&suitedMonoRepo, "suited-mono-repo", "l", false, "是否适配单体仓库结构")
	cmd.Flags().StringVarP(&repoAddr, "repo-addr", "r", "", "Docker 镜像仓库地址，不含 http 和仓库名")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./serverName_http-pb_<时间戳>")

	return cmd
}

type httpPbGenerator struct {
	moduleName   string
	serverName   string
	projectName  string
	protobufFile string
	repoAddr     string
	outPath      string

	suitedMonoRepo bool
}

func (g *httpPbGenerator) generateCode() (string, error) {
	protobufFiles, isImportTypes, err := parseProtobufFiles(g.protobufFile)
	if err != nil {
		return "", err
	}

	subTplName := codeNameHTTPPb
	r := Replacers[TplNameSunshine]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// specify the subdirectory and files
	subDirs := []string{
		"cmd/serverNameExample_httpPbExample", "sunshine/configs",
		"sunshine/deployments", "sunshine/scripts", "sunshine/third_party",
	}
	subFiles := []string{
		"sunshine/.gitignore", "sunshine/.golangci.yml", "sunshine/go.mod", "sunshine/go.sum",
		"sunshine/Jenkinsfile", "sunshine/Makefile", "sunshine/README.md",
	}

	if isImportTypes {
		subFiles = append(subFiles, "api/types/types.proto")
	}

	selectFiles := map[string][]string{
		"docs": {
			"apis.go", "apis.swagger.json",
		},
		"internal/config": {
			"serverNameExample.go",
		},
		"internal/database": {
			"snow.go",
		},
		"internal/ecode": {
			"systemCode_http.go",
		},
		"internal/routers": {
			"routers_pbExample.go",
		},
		"internal/server": {
			"http.go", "http_option.go",
		},
		"internal/cron": {
			"cron.go", "tasks/userExampleCronTask.go",
		},
		"internal/mq": {
			"rabbitmq/mq.go", "rabbitmq/consumers/doingOrder.go",
		},
	}

	if g.suitedMonoRepo {
		subDirs = removeElements(subDirs, "sunshine/third_party")
		subFiles = removeElements(subFiles, "sunshine/go.mod", "sunshine/go.sum", "api/types/types.proto")
	}

	replaceFiles := make(map[string][]string)
	subFiles = append(subFiles, getSubFiles(selectFiles, replaceFiles)...)

	// ignore some directories and files
	ignoreDirs := []string{"cmd/sunshine"}
	ignoreFiles := []string{""}

	r.SetSubDirsAndFiles(subDirs, subFiles...)
	r.SetIgnoreSubDirs(ignoreDirs...)
	r.SetIgnoreSubFiles(ignoreFiles...)
	if setErr := r.SetOutputDir(g.outPath, g.serverName+"_"+subTplName); setErr != nil {
		return "", setErr
	}
	fields := g.addFields(r)
	r.SetReplacementFields(fields)
	if err = r.SaveFiles(); err != nil {
		return "", err
	}

	// Add replace directive to go.mod for local development
	if !g.suitedMonoRepo {
		if err = appendReplaceDirective(r.GetOutputDir(), g.moduleName); err != nil {
			return "", err
		}
	}

	if err = saveProtobufFiles(g.moduleName, g.serverName, g.suitedMonoRepo, r.GetOutputDir(), protobufFiles); err != nil {
		return "", err
	}
	if saveErr := saveGenInfo(g.moduleName, g.serverName, g.suitedMonoRepo, r.GetOutputDir()); saveErr != nil {
		fmt.Printf("save gen info error: %v\n", saveErr)
	}
	if swaggerErr := saveEmptySwaggerJSON(r.GetOutputDir()); swaggerErr != nil {
		fmt.Printf("save empty swagger json error: %v\n", swaggerErr)
	}

	fmt.Printf(`
using help:
  1. open a terminal and execute the command to generate code: make proto
  2. open file internal/handler/xxx.go, replace panic("implement me") according to template code example.
  3. compile and run service: make run
  4. visit http://localhost:8080/apis/swagger/index.html in your browser, and test the http api.

`)
	outpath := r.GetOutputDir()
	fmt.Printf("generate %s's web service code successfully, out = %s\n", g.serverName, outpath)
	return outpath, nil
}

func (g *httpPbGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	repoHost, _ := parseImageRepoAddr(g.repoAddr)

	fields = append(fields, genDeleteMarkFields(r, httpFile, startMark, endMark)...)
	fields = append(fields, genDeleteMarkFields(r, dockerFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, dockerFileBuild, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, dockerComposeFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, k8sDeploymentFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, k8sServiceFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, imageBuildFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, imageBuildLocalFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteAllMarkFields(r, makeFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteMarkFields(r, gitIgnoreFile, wellStartMark, wellEndMark)...)
	fields = append(fields, genDeleteAllMarkFields(r, protoShellFile, wellStartMark, wellEndMark)...)

	//fields = append(fields, genDeleteMarkFields(r, deploymentConfigFile, wellStartMark, wellEndMark)...)
	fields = append(fields, replaceFileContentMark(r, readmeFile,
		setReadmeTitle(g.moduleName, g.serverName, codeNameHTTPPb, g.suitedMonoRepo))...)
	fields = append(fields, []replacer.Field{
		{ // replace the configuration of the *.yml file
			Old: appConfigFileMark,
			New: httpServerConfigCode,
		},
		{ // replace the configuration of the *.yml file
			Old: appConfigFileMark2,
			New: getDBConfigCode(undeterminedDBDriver),
		},
		//{ // replace the contents of the model/init.go file
		//	Old: modelInitDBFileMark,
		//	New: getInitDBCode(DBDriverMysql), // default is mysql
		//},
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
		//	New: getDBConfigCode(DBDriverMysql, true),
		//},
		{ // replace the contents of the *-deployment.yml file
			Old: k8sDeploymentFileMark,
			New: k8sDeploymentFileHTTPCode,
		},
		{ // replace the contents of the *-svc.yml file
			Old: k8sServiceFileMark,
			New: k8sServiceFileHTTPCode,
		},
		{ // replace the contents of the proto.sh file
			Old: protoShellFileGRPCMark,
			New: "",
		},
		{ // replace the contents of the proto.sh file
			Old: protoShellFileMark,
			New: protoShellHandlerCode,
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
			Old: "serverNameExample",
			New: g.serverName,
		},
		// docker image and k8s deployment script replacement
		{
			Old: "server-name-example",
			New: xstrings.ToKebabCase(g.serverName), // convert to kebab-case format
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
			Old: "_httpPbExample",
			New: "",
		},
		{
			Old: "_pbExample",
			New: "",
		},
		{
			Old: "_mixExample",
			New: "",
		},
	}...)

	if g.suitedMonoRepo {
		fs := serverCodeFields(codeNameHTTPPb, g.moduleName, g.serverName)
		fields = append(fields, fs...)
	}

	return fields
}
