// Package generate is to generate code, including model, cache, dao, handler, http, service, grpc, grpc-gw, grpc-cli code.
package generate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/huandu/xstrings"

	"github.com/18721889353/sunshine/pkg/gobash"
	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/replacer"
	"github.com/18721889353/sunshine/pkg/sql2code/parser"
	"github.com/18721889353/sunshine/pkg/utils"
)

const (
	defaultGoModVersion = "go 1.25"

	// TplNameSunshine name of the template
	TplNameSunshine = "sunshine"

	// DBDriverMysql mysql driver
	DBDriverMysql = "mysql"

	undeterminedDBDriver = "undetermined" // used in services created based on protobuf.

	// code name
	codeNameHTTP        = "http"
	codeNameGRPC        = "grpc"
	codeNameHTTPPb      = "http-pb"
	codeNameGRPCPb      = "grpc-pb"
	codeNameGRPCGW      = "grpc-gw-pb"
	codeNameGRPCHTTP    = "grpc-http-pb"
	codeNameHandler     = "handler"
	codeNameHandlerPb   = "handler-pb"
	codeNameService     = "service"
	codeNameServiceHTTP = "service-handler"
	codeNameDao         = "dao"
	codeNameProtobuf    = "protobuf"
	codeNameModel       = "model"
	codeNameGRPCConn    = "grpc-conn"
	codeNameCache       = "cache"

	wellPrefix    = "## "
	pkgPathSuffix = "/pkg"
	apiDocsSuffix = " api docs"
)

var (
	modelFile     = "model/userExample.go"
	modelFileMark = "// todo generate model code to here"

	databaseInitDBFile     = "database/init.go"
	databaseInitDBFileMark = "// todo generate initialisation database code here"

	showDbNameMark = "// todo show db driver name here"
	// CurrentDbDriver 当前数据库驱动标记
	CurrentDbDriver = func(dbDriver string) string { return "// db driver is " + dbDriver }

	cacheFile = "cache/cacheNameExample.go"

	daoFile     = "dao/userExample.go"
	daoFileMark = "// todo generate the update fields code to here"
	daoTestFile = "dao/userExample_test.go"

	typesFile         = "types/userExample_types.go"
	handlerFileMark   = "// todo generate the request and response struct to here"
	handlerTestFile   = "handler/userExample_test.go"
	handlerPbTestFile = "handler/userExample_logic_test.go"

	handlerLogicFile = "handler/userExample_logic.go"
	serviceLogicFile = "service/userExample.go"
	embedTimeMark    = "// todo generate the conversion createdAt and updatedAt code here"

	httpFile = "server/http.go"

	protoFile     = "v1/userExample.proto"
	protoFileMark = "// todo generate the protobuf code here"

	serviceTestFile   = "service/userExample_test.go"
	serviceClientFile = "service/userExample_client_test.go"
	serviceFileMark   = "// todo generate the service struct code here"

	dockerFile     = "scripts/build/Dockerfile"
	dockerFileMark = "# todo generate dockerfile code for http or grpc here"

	dockerFileBuild     = "scripts/build/Dockerfile_build"
	dockerFileBuildMark = "# todo generate dockerfile_build code for http or grpc here"

	imageBuildFile     = "scripts/image-build.sh"
	imageBuildFileMark = "# todo generate image-build code for http or grpc here"

	imageBuildLocalFile     = "scripts/image-build-local.sh"
	imageBuildLocalFileMark = "# todo generate image-build-local code for http or grpc here"

	dockerComposeFile     = "deployments/docker-compose/docker-compose.yml"
	dockerComposeFileMark = "# todo generate docker-compose.yml code for http or grpc here"

	k8sDeploymentFile     = "deployments/kubernetes/serverNameExample-deployment.yml"
	k8sDeploymentFileMark = "# todo generate k8s-deployment.yml code for http or grpc here"

	k8sServiceFile     = "deployments/kubernetes/serverNameExample-svc.yml"
	k8sServiceFileMark = "# todo generate k8s-svc.yml code for http or grpc here"

	protoShellFile         = "scripts/protoc.sh"
	protoShellFileGRPCMark = "# todo generate grpc files here"
	protoShellFileMark     = "# todo generate api template code command here"

	appConfigFileMark  = "# todo generate http or rpc server configuration here"
	appConfigFileMark2 = "# todo generate the database configuration here"

	expectedSQLForDeletion = "expectedSQLForDeletion := \"UPDATE .*\""

	//deploymentConfigFile     = "kubernetes/serverNameExample-configmap.yml"
	//deploymentConfigFileMark = "# todo generate the database configuration for deployment here"

	configmapFileMark = "# todo generate server configuration code here"

	readmeFile    = "sunshine/README.md"
	makeFile      = "sunshine/Makefile"
	gitIgnoreFile = "sunshine/.gitignore"

	startMarkStr  = "// delete the templates code start"
	endMarkStr    = "// delete the templates code end"
	startMark     = []byte(startMarkStr)
	endMark       = []byte(endMarkStr)
	wellStartMark = symbolConvert(startMarkStr)
	wellEndMark   = symbolConvert(endMarkStr)

	// embed FS template file when using
	selfPackageName = "github.com/18721889353/sunshine"
)

var (
	// ModelInitDBFile 模型初始化数据库文件
	ModelInitDBFile = databaseInitDBFile
	// ModelInitDBFileMark 模型初始化数据库文件标记
	ModelInitDBFileMark = databaseInitDBFileMark
	//AppConfigFileDBMark = appConfigFileMark2
)

// StartMark is the start mark used in code generation templates
var StartMark = startMark

// EndMark is the end mark used in code generation templates
var EndMark = endMark

func symbolConvert(str string, additionalChar ...string) []byte {
	char := ""
	if len(additionalChar) > 0 {
		char = additionalChar[0]
	}

	return []byte(strings.Replace(str, "//", "#", 1) + char)
}

func convertServerName(serverName string) string {
	return strings.ReplaceAll(serverName, "-", "_")
}

func convertProjectAndServerName(projectName, serverName string) (pn string, sn string, err error) {
	if strings.HasSuffix(serverName, "-test") {
		err = fmt.Errorf(`the server name (%s) suffix "-test" is not supported for code generation, please delete suffix "-test" or change it to another name. `, serverName)
	}
	if strings.HasSuffix(serverName, "_test") {
		err = fmt.Errorf(`the server name (%s) suffix "_test" is not supported for code generation, please delete suffix "_test" or change it to another name. `, serverName)
	}

	sn = strings.ReplaceAll(serverName, "-", "_")
	pn = xstrings.ToKebabCase(projectName)
	return pn, sn, err
}

func adjustmentOfIDType(handlerCodes string, _ string, isCommonStyle bool) string {
	if isCommonStyle {
		return handlerCodes
	}
	return idTypeToUint64(idTypeFixToUint64(handlerCodes))
}

func idTypeFixToUint64(handlerCodes string) string {
	subStart := "ByIDRequest struct {"
	subEnd := "`" + `json:"id" binding:""` + "`"
	if subBytes := gofile.FindSubBytesNotIn([]byte(handlerCodes), []byte(subStart), []byte(subEnd)); len(subBytes) > 0 {
		old := subStart + string(subBytes) + subEnd
		newStr := subStart + "\n\tID uint64 " + subEnd + " // uint64 id\n"
		handlerCodes = strings.ReplaceAll(handlerCodes, old, newStr)
	}

	return handlerCodes
}

func idTypeToUint64(handlerCodes string) string {
	subStart := "ObjDetail struct {"
	subEnd := "`" + `json:"id"` + "`"
	if subBytes := gofile.FindSubBytesNotIn([]byte(handlerCodes), []byte(subStart), []byte(subEnd)); len(subBytes) > 0 {
		old := subStart + string(subBytes) + subEnd
		newStr := subStart + "\n\tID uint64 " + subEnd + " // convert to uint64 id\n"
		handlerCodes = strings.ReplaceAll(handlerCodes, old, newStr)
	}

	return handlerCodes
}

// idTypeToStr 将ID类型转换为string(暂未使用,保留供将来扩展)
// func idTypeToStr(handlerCodes string) string {
// 	subStart := "ObjDetail struct {"
// 	subEnd := "`" + `json:"id"` + "`"
// 	if subBytes := gofile.FindSubBytesNotIn([]byte(handlerCodes), []byte(subStart), []byte(subEnd)); len(subBytes) > 0 {
// 		old := subStart + string(subBytes) + subEnd
// 		newStr := subStart + "\n\tID string " + subEnd + " // convert to string id\n"
// 		handlerCodes = strings.ReplaceAll(handlerCodes, old, newStr)
// 	}
//
// 	return handlerCodes
// }

func deleteFieldsMark(r replacer.Replacer, filename string, startMark []byte, endMark []byte) []replacer.Field {
	var fields []replacer.Field

	data, err := r.ReadFile(filename)
	if err != nil {
		//fmt.Printf("readFile error: %v\n", err)
		return fields
	}
	if subBytes := gofile.FindSubBytes(data, startMark, endMark); len(subBytes) > 0 {
		fields = append(fields,
			replacer.Field{ // clear marked template code
				Old: string(subBytes),
				New: "",
			},
		)
	}

	return fields
}

// DeleteCodeMark delete code mark fragment
func DeleteCodeMark(r replacer.Replacer, filename string, startMark []byte, endMark []byte) []replacer.Field {
	return deleteFieldsMark(r, filename, startMark, endMark)
}

func deleteAllFieldsMark(r replacer.Replacer, filename string, startMark []byte, endMark []byte) []replacer.Field {
	var fields []replacer.Field

	data, err := r.ReadFile(filename)
	if err != nil {
		//fmt.Printf("readFile error: %v\n", err)
		return fields
	}
	allSubBytes := gofile.FindAllSubBytes(data, startMark, endMark)
	for _, subBytes := range allSubBytes {
		fields = append(fields,
			replacer.Field{ // clear marked template code
				Old: string(subBytes),
				New: "",
			},
		)
	}

	return fields
}

func replaceFileContentMark(r replacer.Replacer, filename string, newContent string) []replacer.Field {
	var fields []replacer.Field

	data, err := r.ReadFile(filename)
	if err != nil {
		fmt.Printf("read the file \"%s\" error: %v\n", filename, err)
		return fields
	}

	fields = append(fields, replacer.Field{
		Old: string(data),
		New: newContent,
	})

	return fields
}

// resolving mirror repository host and name
func parseImageRepoAddr(addr string) (host string, name string) {
	splits := strings.Split(addr, "/")

	// default docker hub official repo address
	if len(splits) == 1 {
		return "https://index.docker.io/v1", addr
	}

	// unofficial repo address
	l := len(splits)
	return strings.Join(splits[:l-1], "/"), splits[l-1]
}

// ------------------------------------------------------------------------------------------

func parseProtobufFiles(protobufFile string) ([]string, bool, error) {
	if filepath.Ext(protobufFile) != ".proto" {
		return nil, false, fmt.Errorf("%v is not a protobuf file", protobufFile)
	}

	protobufFiles := gofile.FuzzyMatchFiles(protobufFile)
	countService, countImportTypes := 0, 0
	for _, file := range protobufFiles {
		protoData, err := os.ReadFile(file)
		if err != nil {
			return nil, false, err
		}
		if isExistServiceName(protoData) {
			countService++
		}
		if isDependImport(protoData, "api/types/types.proto") {
			countImportTypes++
		}
	}

	if countService == 0 {
		return nil, false, errors.New("not found service name, protobuf file requires at least one service")
	}

	return protobufFiles, countImportTypes > 0, nil
}

// ParseFuzzyProtobufFiles parse fuzzy protobuf files
func ParseFuzzyProtobufFiles(protobufFile string) ([]string, error) {
	var protoFiles []string
	ss := strings.Split(protobufFile, ",")
	for _, s := range ss {
		files, _, err := parseProtobufFiles(s)
		if err != nil {
			return nil, err
		}
		protoFiles = append(protoFiles, files...)
	}
	return protoFiles, nil
}

// save the moduleName and serverName to the specified file for external use
func saveGenInfo(moduleName string, serverName string, suitedMonoRepo bool, outputDir string) error {
	genInfo := moduleName + "," + serverName + "," + strconv.FormatBool(suitedMonoRepo)
	dir := outputDir + "/docs"
	if err := os.MkdirAll(dir, 0766); err != nil {
		return fmt.Errorf("create directory %s error: %v", dir, err)
	}
	file := dir + "/gen.info"
	err := os.WriteFile(file, []byte(genInfo), 0666)
	if err != nil {
		return fmt.Errorf("save file %s error, %v", file, err)
	}
	return nil
}

func saveEmptySwaggerJSON(outputDir string) error {
	dir := outputDir + "/docs"
	if err := os.MkdirAll(dir, 0766); err != nil {
		return fmt.Errorf("create directory %s error: %v", dir, err)
	}
	file := dir + "/apis.swagger.json"
	err := os.WriteFile(file, []byte(`{"swagger":"2.0","info":{"version":"version not set"}}`), 0666)
	if err != nil {
		return fmt.Errorf("save file %s error, %v", file, err)
	}
	return nil
}

// get moduleName and serverName from directory
func getNamesFromOutDir(dir string) (moduleName string, serverName string, suitedMonoRepo bool) {
	if dir == "" {
		return "", "", false
	}
	data, err := os.ReadFile(dir + "/docs/gen.info")
	if err != nil {
		return "", "", false
	}

	ms := strings.Split(string(data), ",")
	if len(ms) == 2 {
		return ms[0], ms[1], false
	} else if len(ms) >= 3 {
		return ms[0], ms[1], ms[2] == "true"
	}

	return "", "", false
}

func saveProtobufFiles(moduleName string, serverName string, suitedMonoRepo bool, outputDir string, protobufFiles []string) error {
	if suitedMonoRepo {
		outputDir = strings.TrimSuffix(outputDir, serverName)
		outputDir = strings.TrimSuffix(outputDir, gofile.GetPathDelimiter())
	}

	for _, pbFile := range protobufFiles {
		pbContent, err := os.ReadFile(pbFile)
		if err != nil {
			fmt.Printf("read file %s error, %v\n", pbFile, err)
			continue
		}
		pbContent = replacePackage(pbContent, moduleName, serverName)

		dir := outputDir + "/api/" + serverName + "/v1"
		if mkdirErr := os.MkdirAll(dir, 0766); mkdirErr != nil {
			return fmt.Errorf("create directory %s error: %v", dir, mkdirErr)
		}

		_, name := filepath.Split(pbFile)
		file := dir + "/" + name
		if gofile.IsExists(file) {
			return fmt.Errorf("file %s already exists", file)
		}
		err = os.WriteFile(file, pbContent, 0666)
		if err != nil {
			return fmt.Errorf("save file %s error, %v", file, err)
		}
	}

	return nil
}

func isExistServiceName(data []byte) bool {
	servicePattern := `\nservice (\w+)`
	re := regexp.MustCompile(servicePattern)
	matchArr := re.FindStringSubmatch(string(data))
	return len(matchArr) >= 2
}

func isDependImport(protoData []byte, pkgName string) bool {
	return bytes.Contains(protoData, []byte(pkgName))
}

func replacePackage(data []byte, moduleName string, serverName string) []byte {
	if bytes.Contains(data, []byte("\r\n")) {
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}

	regStr := `\npackage [\w\W]*?;`
	reg := regexp.MustCompile(regStr)
	packageName := reg.Find(data)

	regStr2 := `go_package [\w\W]*?;\n`
	reg2 := regexp.MustCompile(regStr2)
	goPackageName := reg2.Find(data)

	if len(packageName) > 0 {
		newPackage := fmt.Sprintf("\npackage api.%s.v1;", serverName)
		data = bytes.Replace(data, packageName, []byte(newPackage), 1)
	}

	if len(goPackageName) > 0 {
		newGoPackage := fmt.Sprintf("go_package = \"%s/api/%s/v1;v1\";\n", moduleName, serverName)
		data = bytes.Replace(data, goPackageName, []byte(newGoPackage), 1)
	}

	return data
}

func getDBConfigCode(dbDriver string) string {
	dbConfigCode := ""
	switch strings.ToLower(dbDriver) {
	case DBDriverMysql:
		dbConfigCode = mysqlConfigCode
	case undeterminedDBDriver:
		dbConfigCode = undeterminedDatabaseConfigCode
	}
	return dbConfigCode
}

func getInitDBCode(dbDriver string) string {
	initDBCode := ""
	switch strings.ToLower(dbDriver) {
	case DBDriverMysql:
		initDBCode = modelInitDBFileMysqlCode
	default:
		panic("getInitDBCode error, unsupported database driver: " + dbDriver)
	}
	return initDBCode
}

// GetInitDataBaseCode get init db code
func GetInitDataBaseCode(dbDriver string) string {
	return getInitDBCode(dbDriver)
}

func getEmbedTimeCode(isEmbed bool) string {
	if isEmbed {
		return embedTimeCode
	}
	return ""
}

func getExpectedSQLForDeletion(isEmbed bool) string {
	if !isEmbed {
		return strings.ReplaceAll(expectedSQLForDeletion, "UPDATE", "DELETE")
	}

	return expectedSQLForDeletion
}

func getExpectedSQLForDeletionField(isEmbed bool) []replacer.Field {
	var fields []replacer.Field
	esql := getExpectedSQLForDeletion(isEmbed)
	if esql != expectedSQLForDeletion {
		fields = append(fields, []replacer.Field{
			{
				Old: expectedSQLForDeletion,
				New: getExpectedSQLForDeletion(isEmbed),
			},
			{
				Old: "expectedArgsForDeletionTime := d.AnyTime",
				New: "",
			},
			{
				Old: "expectedArgsForDeletionTime := h.MockDao.AnyTime",
				New: "",
			},
			{
				Old: "WithArgs(expectedArgsForDeletionTime, testData.ID)",
				New: "WithArgs(testData.ID)",
			},
		}...)
	}
	return fields
}

func convertYamlConfig(configFile string) (string, error) {
	f, err := os.Open(configFile)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint

	scanner := bufio.NewScanner(f)
	modifiedLines := []string{}

	for scanner.Scan() {
		line := scanner.Text()
		modifiedLine := "    " + line
		modifiedLines = append(modifiedLines, modifiedLine)
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return strings.Join(modifiedLines, "\n"), nil
}

func generateConfigmap(serverName string, outPath string) error {
	configFile := fmt.Sprintf(outPath+"/configs/%s.yml", serverName)
	configmapFile := fmt.Sprintf(outPath+"/deployments/kubernetes/%s-configmap.yml", serverName)
	configFileData, err := convertYamlConfig(configFile)
	if err != nil {
		return err
	}
	configmapFileData, err := os.ReadFile(configmapFile)
	if err != nil {
		return err
	}
	data := strings.ReplaceAll(string(configmapFileData), configmapFileMark, configFileData)
	return os.WriteFile(configmapFile, []byte(data), 0666)
}

func removeElements(slice []string, elements ...string) []string {
	if len(elements) == 0 {
		return slice
	}
	filters := make(map[string]struct{})
	for _, element := range elements {
		filters[element] = struct{}{}
	}
	result := make([]string, 0, len(slice)-1)
	for _, s := range slice {
		if _, ok := filters[s]; !ok {
			result = append(result, s)
		}
	}
	return result
}

func moveProtoFileToAPIDir(moduleName string, serverName string, suitedMonoRepo bool, outputDir string) error {
	apiDir := outputDir + gofile.GetPathDelimiter() + "api"
	protoFiles, err := gofile.ListFiles(apiDir, gofile.WithNoAbsolutePath(), gofile.WithSuffix(".proto"))
	if err != nil {
		fmt.Printf("list proto files error: %v\n", err)
		return err
	}
	if err := saveProtobufFiles(moduleName, serverName, suitedMonoRepo, outputDir, protoFiles); err != nil {
		return err
	}
	time.Sleep(time.Millisecond * 100)
	if removeErr := os.RemoveAll(apiDir); removeErr != nil {
		fmt.Printf("remove api directory error: %v\n", removeErr)
	}
	return nil
}

var (
	// for protoc.sh and protoc-doc.sh
	monoRepoAPIPath = `bash scripts/patch-mono.sh
cd ..

protoBasePath="api"`

	// for patch-mono.sh
	monoRepoHTTPPatch = `bash scripts/patch-mono.sh

HOST_ADDR=$1`

	// for patch.sh
	typePbShellCode = `
    if [ ! -d "../api/types" ]; then
        sunshine patch gen-types-pb --out=./
        checkResult $?
        mv -f api/types ../api
        rmdir api
    fi`

	dupCodeMark = "--dir=internal/ecode"

	adaptDupCode = func(serverType string, serverName string) string {
		if serverType == codeNameHTTP {
			return dupCodeMark
		}
		return fmt.Sprintf("--dir=%s/internal/ecode", serverName)
	}
)

func serverCodeFields(serverType string, moduleName string, serverName string) []replacer.Field {
	return []replacer.Field{
		{
			Old: fmt.Sprintf("\"%s/internal/", moduleName),
			New: fmt.Sprintf("\"%s/internal/", moduleName+"/"+serverName),
		},
		{
			Old: "=$(cat docs/gen.info",
			New: fmt.Sprintf("=$(cat %s/docs/gen.info", serverName),
		},
		{
			Old: dupCodeMark,
			New: adaptDupCode(serverType, serverName),
		},
		{
			Old: fmt.Sprintf("\"%s/cmd/", moduleName),
			New: fmt.Sprintf("\"%s/cmd/", moduleName+"/"+serverName),
		},
		{
			Old: fmt.Sprintf("\"%s/configs", moduleName),
			New: fmt.Sprintf("\"%s/configs", moduleName+"/"+serverName),
		},
		{
			Old: fmt.Sprintf("\"%s/docs", moduleName),
			New: fmt.Sprintf("\"%s/docs", moduleName+"/"+serverName),
		},
		{
			Old: fmt.Sprintf("\"%s/api", moduleName),
			New: fmt.Sprintf("\"%s/api", moduleName+"/"+serverName),
		},
		{
			Old: "merge_file_name=docs/apis.json",
			New: fmt.Sprintf("merge_file_name=%s/docs/apis.json", serverName),
		},
		{
			Old: "--file=docs/apis.swagger.json",
			New: fmt.Sprintf("--file=%s/docs/apis.swagger.json", serverName),
		},
		{
			Old: "sunshine merge http-pb",
			New: fmt.Sprintf("sunshine merge http-pb --dir=%s", serverName),
		},
		{
			Old: "sunshine merge rpc-pb",
			New: fmt.Sprintf("sunshine merge rpc-pb --dir=%s", serverName),
		},
		{
			Old: "sunshine merge rpc-gw-pb",
			New: fmt.Sprintf("sunshine merge rpc-gw-pb --dir=%s", serverName),
		},
		{
			Old: "docs/apis.html",
			New: fmt.Sprintf("%s/docs/apis.html", serverName),
		},
		{
			Old: `sunshine patch gen-types-pb --out=./`,
			New: typePbShellCode,
		},
		{
			Old: `protoBasePath="api"`,
			New: monoRepoAPIPath,
		},
		{
			Old: `HOST_ADDR=$1`,
			New: monoRepoHTTPPatch,
		},
		{
			Old: `genServerType=$1`,
			New: fmt.Sprintf(`genServerType="%s"`, serverType),
		},
		{
			Old: fmt.Sprintf("go get %s@", moduleName),
			New: fmt.Sprintf("go get %s@", "github.com/18721889353/sunshine"),
		},
	}
}

// SubServerCodeFields sub server code fields
func SubServerCodeFields(moduleName string, serverName string) []replacer.Field {
	return []replacer.Field{
		{
			Old: fmt.Sprintf("\"%s/internal/", moduleName),
			New: fmt.Sprintf("\"%s/internal/", moduleName+"/"+serverName),
		},
		{
			Old: fmt.Sprintf("\"%s/configs", moduleName),
			New: fmt.Sprintf("\"%s/configs", moduleName+"/"+serverName),
		},
		{
			Old: fmt.Sprintf("\"%s/api", moduleName),
			New: fmt.Sprintf("\"%s/api", moduleName+"/"+serverName),
		},
	}
}

func changeOutPath(outPath string, serverName string) string {
	switch outPath {
	case "", ".", "./", ".\\", serverName, "./" + serverName, ".\\" + serverName:
		return serverName
	}
	return outPath + gofile.GetPathDelimiter() + serverName
}

// getSubFiles 收集需要生成的所有文件路径列表。
// 自动确保 internal/config 目录包含 nacos.go 和 register_helper.go，支持通过 replaceFiles 替换选定文件。
// 参数:
//   - selectFiles: 按目录分组的待生成文件映射，key 为目录路径，value 为文件名列表。
//   - replaceFiles: 可选的文件替换映射，会覆盖 selectFiles 中同目录的文件列表。
//
// 返回值:
//   - 拼接后的完整文件路径列表（格式: 目录/文件名）。
func getSubFiles(selectFiles map[string][]string, replaceFiles map[string][]string) []string {
	files := []string{}
	// 所有生成器自动包含 nacos.go（配置中心拉取功能）、nacos_encrypt.go（Nacos凭据加解密）和 register_helper.go（配置构建辅助方法）
	if v, ok := selectFiles["internal/config"]; ok {
		hasNacos := false
		hasNacosEncrypt := false
		hasRegisterHelper := false
		for _, f := range v {
			if f == "nacos.go" {
				hasNacos = true
			}
			if f == "nacos_encrypt.go" {
				hasNacosEncrypt = true
			}
			if f == "register_helper.go" {
				hasRegisterHelper = true
			}
		}
		if !hasNacos {
			selectFiles["internal/config"] = append(v, "nacos.go")
		}
		if !hasNacosEncrypt {
			selectFiles["internal/config"] = append(selectFiles["internal/config"], "nacos_encrypt.go")
		}
		if !hasRegisterHelper {
			selectFiles["internal/config"] = append(selectFiles["internal/config"], "register_helper.go")
		}
	}

	for dir, filenames := range selectFiles {
		if v, ok := replaceFiles[dir]; ok {
			filenames = v
		}
		for _, filename := range filenames {
			files = append(files, dir+"/"+filename)
		}
	}
	return files
}

// Version 版本信息结构
type Version struct {
	major     string
	minor     string
	patch     string
	goVersion string
}

func getLocalGoVersion() string {
	result, err := gobash.Exec("go", "version")
	if err != nil {
		return defaultGoModVersion
	}

	versions := []string{
		strings.ReplaceAll(defaultGoModVersion, " ", ""),
		string(result),
	}

	versionRegex := regexp.MustCompile(`go(\d+)\.(\d+)(\.(\d+))?`)

	var versionList []Version
	for _, v := range versions {
		matches := versionRegex.FindStringSubmatch(v)
		if len(matches) >= 3 {
			goVersion := "go " + matches[1] + "." + matches[2]
			if matches[4] != "" {
				goVersion += "." + matches[4]
			}
			versionList = append(versionList, Version{major: matches[1], minor: matches[2], patch: matches[4], goVersion: goVersion})
		}
	}

	//  descending sort by major, minor, patch
	sort.Slice(versionList, func(i, j int) bool {
		if versionList[i].major != versionList[j].major {
			return utils.StrToInt(versionList[i].major) > utils.StrToInt(versionList[j].major)
		}
		if versionList[i].minor != versionList[j].minor {
			return utils.StrToInt(versionList[i].minor) > utils.StrToInt(versionList[j].minor)
		}
		return utils.StrToInt(versionList[i].patch) > utils.StrToInt(versionList[j].patch)
	})

	if len(versionList) == 0 {
		return defaultGoModVersion
	}

	return versionList[0].goVersion
}

func dbDriverErr(driver string) error {
	return errors.New("unsupported db driver: " + driver)
}

func flagTip(name ...string) string {
	if len(name) == 2 {
		return fmt.Sprintf("if you specify the directory where the web or microservice generated by sunshine, the %s and %s flag can be ignored", name[0], name[1])
	}
	return fmt.Sprintf("if you specify the directory where the web or microservice generated by sunshine, the %s flag can be ignored", name[0])
}

func cutPath(srcFilePath string) string {
	dirPath, err := filepath.Abs(".")
	if err != nil {
		fmt.Printf("get absolute path error: %v\n", err)
		return srcFilePath
	}
	srcFilePath = strings.ReplaceAll(srcFilePath, dirPath, ".")
	return strings.ReplaceAll(srcFilePath, "\\", "/")
}

func wrapPoint(s string) string {
	return "`" + s + "`"
}

// setReadmeTitle 生成 README.md 的标题和项目信息表格内容。
// 参数:
//   - moduleName: Go 模块名称。
//   - serverName: 服务名称。
//   - serverType: 服务类型（http / grpc / http-pb 等）。
//   - suitedMonoRepo: 是否为单体仓库模式。
// 返回值:
//   - 格式化后的 Markdown 标题和表格字符串，包含服务名称、类型、模块名和仓库类型。
//
// 仓库类型逻辑:
//   - mono-repo: suitedMonoRepo 为 true。
//   - monolith: 非单体仓库且服务类型为 HTTP。
//   - multi-repo: 其他情况。
func setReadmeTitle(moduleName string, serverName string, serverType string, suitedMonoRepo bool) string {
	var repoType string
	if suitedMonoRepo {
		repoType = "mono-repo"
	} else {
		if serverType == codeNameHTTP {
			repoType = "monolith"
		} else {
			repoType = "multi-repo"
		}
	}

	return wellPrefix + serverName + fmt.Sprintf(`

| Feature             | Value          |
| :----------------: | :-----------: |
| Server name      |  %s   |
| Server type        |  %s   |
| Go module name |  %s  |
| Repository type   |  %s  |

`, wrapPoint(serverName), wrapPoint(serverType), wrapPoint(moduleName), wrapPoint(repoType))
}

// GetGoModFields 返回 go.mod 文件中需要替换的字段列表，用于将模板项目中的包路径和 Go 版本替换为实际值。
// 参数:
//   - moduleName: 目标项目的 Go 模块名称，用于替换默认的 sunshine 包路径。
// 返回值:
//   - 包含两个替换字段：sunshine 包路径替换为 moduleName，默认 Go 版本替换为本地版本。
func GetGoModFields(moduleName string) []replacer.Field {
	return []replacer.Field{
		{
			Old: "github.com/18721889353/sunshine",
			New: moduleName,
		},
		{
			Old: defaultGoModVersion,
			New: getLocalGoVersion(),
		},
	}
}

// appendReplaceDirective 在 go.mod 文件中追加 replace 指令，使生成的项目在开发时指向本地 sunshine 源码。
// 参数:
//   - outputDir: 生成项目的输出目录。
//   - _: 保留参数，暂未使用。
// 返回值:
//   - 如果通过编译后的二进制启动（SUNSHINE_COMPILED_BINARY=true）或 sunshine 非本地源码安装，
//     则不添加 replace 指令，返回 nil。
//   - 如果文件写入失败，返回对应的错误。
//
// 仅在以下条件同时满足时才会添加 replace 指令:
//  1. 不是通过编译后二进制启动的 Web UI。
//  2. sunshine 目录包含 cmd、pkg、internal 子目录（即本地源码运行）。
func appendReplaceDirective(outputDir string, _ string) error {
	// 检测是否通过编译后的二进制启动的 Web UI，如果是则不添加 replace 指令
	if os.Getenv("SUNSHINE_COMPILED_BINARY") == "true" {
		return nil
	}

	// 检测 sunshine 是否从本地源码运行
	// 如果 SunshineDir 包含 cmd、pkg、internal 等目录，说明是源码目录
	if !gofile.IsExists(filepath.Join(SunshineDir, "cmd")) ||
		!gofile.IsExists(filepath.Join(SunshineDir, "pkg")) ||
		!gofile.IsExists(filepath.Join(SunshineDir, "internal")) {
		// 不是源码目录，是通过 go install 安装的，不添加 replace 指令
		return nil
	}

	// 是源码目录，添加 replace 指令以指向本地路径
	// Windows 使用反斜杠，Linux/Mac 使用正斜杠
	var sunshinePath string
	if gofile.IsWindows() {
		sunshinePath = SunshineDir
	} else {
		sunshinePath = filepath.ToSlash(SunshineDir)
	}

	goModFile := outputDir + gofile.GetPathDelimiter() + "go.mod"
	replaceContent := fmt.Sprintf("\n// Replace sunshine module to local path for development\nreplace github.com/18721889353/sunshine => %s\n", sunshinePath)

	// Append to go.mod file
	f, err := os.OpenFile(goModFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = f.WriteString(replaceContent)
	return err
}

// unmarshalCrudInfo 将 JSON 字符串反序列化为 CRUD 元数据对象。
// 参数:
//   - str: CRUD 信息的 JSON 字符串。
//
// 返回值:
//   - 解析后的 CrudInfo 指针。
//   - 如果字符串为空或反序列化失败，返回对应的错误。
func unmarshalCrudInfo(str string) (*parser.CrudInfo, error) {
	if str == "" {
		return nil, errors.New("crud info is empty")
	}
	crudInfo := &parser.CrudInfo{}
	err := json.Unmarshal([]byte(str), crudInfo)
	if err != nil {
		return nil, err
	}
	return crudInfo, nil
}

// SetSelectFiles 根据数据库驱动类型设置待生成的基础文件列表到 selectFiles 映射中。
// 参数:
//   - dbDriver: 数据库驱动类型（当前仅支持 "mysql"）。
//   - selectFiles: 待生成文件的目录到文件名的映射，函数会向其中添加依赖的基础文件。
//
// 返回值:
//   - 如果驱动类型不支持，返回对应的错误信息。
//
// nolint
func SetSelectFiles(dbDriver string, selectFiles map[string][]string) error {
	dbDriver = strings.ToLower(dbDriver)
	switch dbDriver {
	case DBDriverMysql:
		selectFiles["internal/database"] = []string{"init.go", "redis.go", "mysql.go", "snow.go", "goRabbitmq.go", "es.go"}
		selectFiles["internal/consts"] = []string{"constants.go"}
	default:
		return errors.New("unsupported db driver: " + dbDriver)
	}
	return nil
}
