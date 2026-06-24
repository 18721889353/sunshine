package parse

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

// Field 表示消息字段
type Field struct {
	Name      string // 字段名称
	FieldType string // 字段类型
	Comment   string // 字段注释
}

// GoTypeZero 返回给定类型的默认零值
func (r Field) GoTypeZero() string {
	switch r.FieldType {
	case "bool":
		return "false"
	case "int32", "uint32", "sint32", "int64", "uint64", "sint64", "sfixed32", "fixed32", "sfixed64", "fixed64":
		return "0"
	case "float", "double":
		return "0.0"
	case "string":
		return `""`
	default:
		return "nil"
	}
}

// ServiceMethod 表示 RPC 方法的字段
type ServiceMethod struct {
	MethodName    string // 方法名称，例如 Create
	Request       string // 请求消息类型，例如 CreateRequest
	RequestFields []*Field
	Reply         string // 响应消息类型，例如 CreateReply
	ReplyFields   []*Field
	Comment       string // 注释，例如 Create a record
	Prompt        string // from comments, used in AI assistant
	InvokeType    int    // 调用类型: 0-单次调用, 1-客户端流式, 2-服务端流式, 3-双向流式

	ServiceName         string // 服务名称，例如 Greeter
	LowerServiceName    string // 服务名称首字母小写，例如 greeter
	LowerCutServiceName string // 去掉 Service 后缀的服务名称首字母小写，例如 GreeterService --> greeter

	// HTTP 规则
	Path   string // 请求路径
	Method string // HTTP 方法
	Body   string

	IsPassGinContext   bool // 是否传递 gin.Context
	IsIgnoreShouldBind bool // 是否忽略 ShouldBindXXX

	IsWebSocket bool // 是否是 WebSocket

	RequestImportPkgName string // 请求消息的导入包名称，例如 userV1
	ReplyImportPkgName   string // 响应消息的导入包名称，例如 userV1
	ProtoPkgName         string // proto 文件的包名称，例如 userV1
}

// AddOne 计数器加一
func (t *ServiceMethod) AddOne(i int) int {
	return i + 1
}

// PbService 表示服务的字段
type PbService struct {
	Name      string           // 服务名称，例如 Greeter
	LowerName string           // 服务名称首字母小写，例如 greeter
	Methods   []*ServiceMethod // 服务的方法

	CutServiceName      string // 去掉 Service 后缀的服务名称，例如 GreeterService --> Greeter
	LowerCutServiceName string // 去掉 Service 后缀的服务名称首字母小写，例如 GreeterService --> greeter

	ImportPkgMap map[string]string // 导入包映射，例如 [userV1]:[userV1 "user/api/user/v1"]

	ProtoFileDir string // proto 文件目录，例如 api/user/v1
	ProtoPkgName string // proto 文件的包名称，例如 userV1
	ModuleName   string // 模块名称
}

// RandNumber 返回 1 到 100 之间的随机数
func (s *PbService) RandNumber() int {
	return rand.Intn(99) + 1
}

// parsePbService 解析单个服务
// 参数:
//   - s: *protogen.Service, 即当前解析的服务
//   - protoFileDir: string, proto 文件目录
//   - moduleName: string, 模块名称
//
// 返回值:
//   - *PbService, 解析后的服务对象
func parsePbService(s *protogen.Service, protoFileDir string, moduleName string) *PbService {
	protoPkgName := convertToPkgName(protoFileDir)
	cutServiceName := getCutServiceName(s.GoName)
	importPkgMap := map[string]string{}

	var methods []*ServiceMethod
	for _, m := range s.Methods {
		rpcMethod := &RPCMethod{} //nolint
		rule, ok := proto.GetExtension(m.Desc.Options(), annotations.E_Http).(*annotations.HttpRule)
		if rule != nil && ok {
			rpcMethod = buildHTTPRule(m, rule, protoPkgName)
		} /*else {
			// 如果没有设置 HTTP 方法和路径，则设置默认值
			//rpcMethod = defaultMethod(m)
		}*/

		requestImportPkgName := convertToPkgName(m.Input.GoIdent.GoImportPath.String())
		replyImportPkgName := convertToPkgName(m.Output.GoIdent.GoImportPath.String())
		if requestImportPkgName != "" {
			importPkgMap[requestImportPkgName] = requestImportPkgName + " " + m.Input.GoIdent.GoImportPath.String()
		}
		if replyImportPkgName != "" {
			importPkgMap[replyImportPkgName] = replyImportPkgName + " " + m.Output.GoIdent.GoImportPath.String()
		}

		comment := getMethodComment(m)
		methods = append(methods, &ServiceMethod{
			MethodName:    m.GoName,
			Request:       m.Input.GoIdent.GoName,
			RequestFields: getFields(m.Input),
			Reply:         m.Output.GoIdent.GoName,
			ReplyFields:   getFields(m.Output),
			Comment:       comment,
			Prompt:        getPrompt(m, comment),
			InvokeType:    getInvokeType(m.Desc.IsStreamingClient(), m.Desc.IsStreamingServer()),

			ServiceName:         s.GoName,
			LowerServiceName:    strings.ToLower(s.GoName[:1]) + s.GoName[1:],
			LowerCutServiceName: strings.ToLower(cutServiceName[:1]) + cutServiceName[1:],

			Path:   rpcMethod.Path,
			Method: rpcMethod.Method,
			Body:   rpcMethod.Body,

			IsPassGinContext:     rpcMethod.IsPassGinContext,
			IsIgnoreShouldBind:   rpcMethod.IsIgnoreShouldBind,
			IsWebSocket:          rpcMethod.IsWebSocket,
			RequestImportPkgName: requestImportPkgName,
			ReplyImportPkgName:   replyImportPkgName,
			ProtoPkgName:         protoPkgName,
		})
	}

	return &PbService{
		Name:                s.GoName,
		LowerName:           strings.ToLower(s.GoName[:1]) + s.GoName[1:],
		Methods:             methods,
		CutServiceName:      cutServiceName,
		LowerCutServiceName: strings.ToLower(cutServiceName[:1]) + cutServiceName[1:],
		ImportPkgMap:        importPkgMap,
		ProtoFileDir:        protoFileDir,
		ProtoPkgName:        protoPkgName,
		ModuleName:          moduleName,
	}
}

// GetServices 解析所有服务
// 参数:
//   - file: *protogen.File, 即当前解析的 proto 文件
//   - moduleName: string, 模块名称
//
// 返回值:
//   - []*PbService, 解析后的所有服务对象
func GetServices(file *protogen.File, moduleName string) []*PbService {
	protoFileDir := getProtoFileDir(file.GeneratedFilenamePrefix)
	var pss []*PbService
	for _, s := range file.Services {
		pss = append(pss, parsePbService(s, protoFileDir, moduleName))
	}
	return pss
}

// getCutServiceName 去掉服务名称中的 "Service" 后缀
// 参数:
//   - name: string, 服务名称
//
// 返回值:
//   - string, 去掉 "Service" 后缀的服务名称
func getCutServiceName(name string) string {
	service := "Service"
	if len(name) < len(service) {
		return name
	}
	l := len(name) - len(service)
	if name[l:] == service {
		if name[:l] == "" {
			return name
		}
		return name[:l]
	}
	return name
}

// getFields 获取消息的所有字段
// 参数:
//   - m: *protogen.Message, 即当前解析的消息
//
// 返回值:
//   - []*Field, 消息的所有字段
func getFields(m *protogen.Message) []*Field {
	var fields []*Field
	for _, f := range m.Fields {
		fieldType := f.Desc.Kind().String()
		if f.Desc.Cardinality().String() == "repeated" {
			fieldType = "[]" + fieldType
		}
		fields = append(fields, &Field{
			Name:      f.GoName,
			FieldType: fieldType,
			Comment:   getFieldComment(f.Comments),
		})
	}
	return fields
}

// getMethodComment 获取方法的注释
// 参数:
//   - m: *protogen.Method, 即当前解析的方法
//
// 返回值:
//   - string, 方法的注释
func getMethodComment(m *protogen.Method) string {
	symbol := "// "
	symbolLen := len(symbol)
	commentPrefix := symbol + m.GoName + " "
	comment := m.Comments.Leading.String()

	if len(comment) >= symbolLen {
		if comment[:symbolLen] == symbol {
			if comment[len(comment)-1] == '\n' {
				comment = comment[:len(comment)-1]
			}
			if len(comment) >= symbolLen {
				if len(comment[symbolLen:]) > len(m.GoName) {
					commentPrefixLower := strings.ToLower(comment[symbolLen : len(m.GoName)+symbolLen+1])
					if commentPrefixLower == strings.ToLower(m.GoName+" ") {
						return commentPrefix + comment[symbolLen+len(m.GoName)+1:]
					}
				}
				return commentPrefix + comment[symbolLen:]
			}
		}
	}

	return commentPrefix + "......"
}

func getPrompt(m *protogen.Method, comment string) string {
	if strings.HasSuffix(comment, "......") {
		return "prompt: implement me"
	}
	prompt := strings.TrimPrefix(comment, "// "+m.GoName)
	prompt = strings.TrimSpace(prompt)
	prompt = strings.ReplaceAll(prompt, "\n//", " ")
	prompt = strings.ReplaceAll(prompt, "\r//", " ")
	prompt = strings.ReplaceAll(prompt, "\r\n//", " ")
	return "prompt: " + prompt
}

// getFieldComment 获取字段的注释
// 参数:
//   - commentSet: protogen.CommentSet, 字段的注释集合
//
// 返回值:
//   - string, 字段的注释
func getFieldComment(commentSet protogen.CommentSet) string {
	comment1 := getFieldCommentStr(commentSet.Leading.String())
	comment2 := getFieldCommentStr(commentSet.Trailing.String())
	if comment1 == "" {
		return comment2
	}
	return comment1 + " " + comment2
}

// getFieldCommentStr 获取字段的注释字符串
// 参数:
//   - comment: string, 字段的注释字符串
//
// 返回值:
//   - string, 处理后的字段注释字符串
func getFieldCommentStr(comment string) string {
	if len(comment) > 2 && comment[len(comment)-1] == '\n' {
		return comment[:len(comment)-1]
	}
	return comment
}

// getInvokeType 获取调用类型
// 参数:
//   - isStreamingClient: bool, 是否客户端流式
//   - isStreamingServer: bool, 是否服务端流式
//
// 返回值:
//   - int, 调用类型: 0-单次调用, 1-客户端流式, 2-服务端流式, 3-双向流式
func getInvokeType(isStreamingClient bool, isStreamingServer bool) int {
	if isStreamingClient {
		if isStreamingServer {
			return 3 // 双向流式
		}
		return 1 // 客户端流式
	}

	if isStreamingServer {
		return 2 // 服务端流式
	}

	return 0 // 单次调用
}

// getProtoFileDir 获取 proto 文件目录
// 参数:
//   - protoPath: string, proto 文件路径
//
// 返回值:
//   - string, proto 文件目录
func getProtoFileDir(protoPath string) string {
	ss := strings.Split(protoPath, "/")
	if len(ss) > 1 {
		return strings.Join(ss[:len(ss)-1], "/")
	}
	return ""
}

// convertToPkgName 将导入路径转换为包名称
// 参数:
//   - importPath: string, 导入路径
//
// 返回值:
//   - string, 包名称
func convertToPkgName(importPath string) string {
	importPath = strings.ReplaceAll(importPath, `"`, "")
	ss := strings.Split(importPath, "/")
	l := len(ss)
	if l > 1 {
		pkgName := strings.ToLower(ss[l-1])
		if isVersionNum(pkgName) || pkgName == "pb" || len(pkgName) < 2 {
			return removeMiddleLine(ss[l-2]) + strings.ToUpper(pkgName[:1]) + pkgName[1:]
		}
		return removeMiddleLine(ss[l-1])
	}
	return ""
}

// isVersionNum 检查是否为版本号
// 参数:
//   - pkgName: string, 包名称
//
// 返回值:
//   - bool, 是否为版本号
func isVersionNum(pkgName string) bool {
	pattern := `^v\d+$`
	matched, err := regexp.MatchString(pattern, pkgName)
	if err != nil {
		return false
	}
	return matched
}

// removeMiddleLine 移除字符串中的中间连字符
// 参数:
//   - str: string, 输入字符串
//
// 返回值:
//   - string, 移除中间连字符后的字符串
func removeMiddleLine(str string) string {
	return strings.ReplaceAll(str, "-", "")
}

// GetImportPkg 获取导入包
// 参数:
//   - services: []*PbService, 服务列表
//
// 返回值:
//   - []byte, 导入包字符串
func GetImportPkg(services []*PbService) []byte {
	pkgMap := make(map[string]string)
	protoFileDir := ""
	moduleName := ""

	for _, service := range services {
		for key, val := range service.ImportPkgMap {
			pkgMap[key] = val
			protoFileDir = service.ProtoFileDir
			moduleName = service.ModuleName
		}
	}

	pkgName := convertToPkgName(protoFileDir)
	selfPkgPath := fmt.Sprintf(`%s "%s"`, pkgName, moduleName+"/"+protoFileDir)
	if _, ok := pkgMap[pkgName]; ok {
		pkgMap[pkgName] = selfPkgPath // 真实包路径优先
	}

	var importPkg []string
	for _, v := range pkgMap {
		importPkg = append(importPkg, v)
	}
	if len(importPkg) == 0 {
		return []byte("")
	}

	return []byte(strings.Join(importPkg, "\n\t"))
}

// GetSourceImportPkg 获取源导入包
// 参数:
//   - services: []*PbService, 服务列表
//
// 返回值:
//   - []byte, 源导入包字符串
func GetSourceImportPkg(services []*PbService) []byte {
	pkgMap := make(map[string]string)
	protoFileDir := ""
	moduleName := ""

	for _, service := range services {
		for key, val := range service.ImportPkgMap {
			pkgMap[key] = val
			protoFileDir = service.ProtoFileDir
			moduleName = service.ModuleName
			break
		}
	}

	pkgName := convertToPkgName(protoFileDir)
	return []byte(fmt.Sprintf(`%s "%s"`, pkgName, moduleName+"/"+protoFileDir))
}

// -------------------------------------------------------------------------------------------

// HTTPPbService 表示 HTTP 服务的字段
type HTTPPbService struct {
	Name      string // 服务名称，例如 Greeter
	LowerName string // 服务名称首字母小写，例如 greeter

	Methods       []*RPCMethod // 服务的方法
	UniqueMethods []*RPCMethod

	ImportPkgMap map[string]string // 导入包映射，例如 [userV1]:[userV1 "user/api/user/v1"]
}

// HTTPPbServices HTTP协议缓冲区服务列表
type HTTPPbServices []*HTTPPbService

// Services 解析所有 HTTP 服务
// 参数:
//   - file: *protogen.File, 即当前解析的 proto 文件
//
// 返回值:
//   - []*HTTPPbService, 解析后的所有 HTTP 服务对象
func Services(file *protogen.File) []*HTTPPbService {
	// 获取文件的 Go 导入路径
	goImportPath := file.GoImportPath.String()
	// 初始化一个 HTTPPbService 列表
	var pss []*HTTPPbService
	// 遍历文件中的每个服务
	for _, s := range file.Services {
		// 初始化导入包映射
		importPkgMap := map[string]string{}
		// 初始化方法列表
		var methods []*RPCMethod
		// 遍历服务中的每个方法
		for _, m := range s.Methods {
			// 获取方法的详细信息
			ms := GetMethods(m, goImportPath)
			// 遍历获取到的方法列表
			for _, method := range ms {
				// 遍历方法的导入包路径
				for pkgPath := range method.ImportPkgPaths {
					// 将导入包路径转换为包名
					pkgName := convertToPkgName(pkgPath)
					// 将包名和路径添加到导入包映射中
					importPkgMap[pkgName] = pkgName + " " + pkgPath
				}
			}
			// 将方法列表追加到当前服务的方法列表中
			methods = append(methods, ms...)
		}
		// 创建 HTTPPbService 结构体并添加到列表中
		pss = append(pss, &HTTPPbService{
			Name:          s.GoName,                                     // 服务名称
			LowerName:     strings.ToLower(s.GoName[:1]) + s.GoName[1:], // 小写的名称
			Methods:       methods,                                      // 方法列表
			UniqueMethods: removeDuplicates(methods),                    // 唯一的方法列表
			ImportPkgMap:  importPkgMap,                                 // 导入包映射
		})
	}

	// 返回解析后的 HTTPPbService 列表
	return pss
}

// MergeImportPkgPath merge import package path
func (services HTTPPbServices) MergeImportPkgPath() string {
	pkgMap := make(map[string]string)

	for _, service := range services {
		for key, val := range service.ImportPkgMap {
			pkgMap[key] = val
		}
	}

	var importPkg []string
	for _, v := range pkgMap {
		importPkg = append(importPkg, v)
	}

	if len(importPkg) == 0 {
		return ""
	}

	return strings.Join(importPkg, "\n\t")
}

func removeDuplicates(methods []*RPCMethod) []*RPCMethod {
	var uniqueMethods []*RPCMethod
	methodMap := make(map[string]struct{})
	for _, method := range methods {
		if _, ok := methodMap[method.Name]; !ok {
			methodMap[method.Name] = struct{}{}
			uniqueMethods = append(uniqueMethods, method)
		}
	}
	return uniqueMethods
}
