// Package parse 提供 Protobuf 方法解析和 HTTP 规则构建功能。
// 该包用于从 Protobuf 定义中提取 RPC 方法信息并转换为 HTTP 路由规则。
package parse

import (
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

// methodSets 用于记录每个方法的调用次数
var methodSets = make(map[string]int)

// GetMethods 获取 RPC 方法描述
// 参数:
//   - m: *protogen.Method, 即当前解析的 RPC 方法
//   - protoSelfPkgPath: string, 当前 proto 文件的包路径
//
// 返回值:
//   - []*RPCMethod, 包含所有解析出的 HTTP 规则对应的 RPC 方法描述
func GetMethods(m *protogen.Method, protoSelfPkgPath string) []*RPCMethod {
	var methods []*RPCMethod

	// 获取 HTTP 规则配置
	rule, ok := proto.GetExtension(m.Desc.Options(), annotations.E_Http).(*annotations.HttpRule)
	if rule != nil && ok {
		// 如果有额外的绑定规则，则逐个解析并添加到 methods 中
		for _, bind := range rule.AdditionalBindings {
			methods = append(methods, buildHTTPRule(m, bind, protoSelfPkgPath))
		}
		// 解析主规则并添加到 methods 中
		methods = append(methods, buildHTTPRule(m, rule, protoSelfPkgPath))
		return methods
	}

	return methods
}

// buildHTTPRule 根据 HTTP 规则构建 RPC 方法描述
// 参数:
//   - m: *protogen.Method, 即当前解析的 RPC 方法
//   - rule: *annotations.HttpRule, 当前解析的 HTTP 规则
//   - protoSelfPkgPath: string, 当前 proto 文件的包路径
//
// 返回值:
//   - *RPCMethod, 构建好的 RPC 方法描述
func buildHTTPRule(m *protogen.Method, rule *annotations.HttpRule, protoSelfPkgPath string) *RPCMethod {
	var (
		path       string
		method     string
		customKind string
		selector   = rule.Selector
	)

	// 根据 HTTP 规则的不同模式设置 path 和 method
	switch pattern := rule.Pattern.(type) {
	case *annotations.HttpRule_Get:
		path = pattern.Get
		method = http.MethodGet
	case *annotations.HttpRule_Put:
		path = pattern.Put
		method = http.MethodPut
	case *annotations.HttpRule_Post:
		path = pattern.Post
		method = http.MethodPost
	case *annotations.HttpRule_Delete:
		path = pattern.Delete
		method = http.MethodDelete
	case *annotations.HttpRule_Patch:
		path = pattern.Patch
		method = http.MethodPatch
	case *annotations.HttpRule_Custom:
		path = pattern.Custom.Path
		customKind = strings.ToLower(pattern.Custom.Kind)
		method = http.MethodPost // 默认为 POST
	}

	// 构建方法描述
	md := buildMethodDesc(m, method, path, customKind, selector, protoSelfPkgPath)
	return md
}

// buildMethodDesc 构建 RPC 方法描述的详细信息
// 参数:
//   - m: *protogen.Method, 即当前解析的 RPC 方法
//   - httpMethod: string, HTTP 方法类型
//   - path: string, 请求路径
//   - customKind: string, 自定义类型
//   - selector: string, 选择器
//   - protoSelfPkgPath: string, 当前 proto 文件的包路径
//
// 返回值:
//   - *RPCMethod, 构建好的 RPC 方法描述
func buildMethodDesc(m *protogen.Method, httpMethod, path string, customKind string, selector string, protoSelfPkgPath string) *RPCMethod {
	defer func() {
		methodSets[m.GoName]++
	}()

	// 记录导入包路径
	importPkgPaths := make(map[string]struct{})
	requestImportPkgName := ""
	replyImportPkgName := ""
	if m.Input.GoIdent.GoImportPath.String() != protoSelfPkgPath {
		requestImportPkgName = convertToPkgName(m.Input.GoIdent.GoImportPath.String()) + "."
		importPkgPaths[m.Input.GoIdent.GoImportPath.String()] = struct{}{}
	}
	if m.Output.GoIdent.GoImportPath.String() != protoSelfPkgPath {
		replyImportPkgName = convertToPkgName(m.Output.GoIdent.GoImportPath.String()) + "."
		importPkgPaths[m.Output.GoIdent.GoImportPath.String()] = struct{}{}
	}

	// 创建 RPC 方法描述对象
	md := &RPCMethod{
		Name:       m.GoName,
		Num:        methodSets[m.GoName],
		Request:    m.Input.GoIdent.GoName,
		Reply:      m.Output.GoIdent.GoName,
		InvokeType: getInvokeType(m.Desc.IsStreamingClient(), m.Desc.IsStreamingServer()),

		// HTTP 规则相关信息
		Path:         path,
		Method:       httpMethod,
		Body:         "",
		ResponseBody: "",

		CustomKind: customKind,
		Selector:   selector,

		// 是否传递 gin.Context
		IsPassGinContext: false,
		// 是否忽略 ShouldBindXXX
		IsIgnoreShouldBind: false,

		RequestImportPkgName: requestImportPkgName,
		ReplyImportPkgName:   replyImportPkgName,
		ProtoSelfPkgPath:     protoSelfPkgPath,
		ImportPkgPaths:       importPkgPaths,
	}

	// 检查并设置自定义类型
	md.checkCustomKind()
	// 检查并设置选择器
	md.checkSelector()
	// 初始化路径参数
	md.InitPathParams()
	return md
}

// RPCMethod 描述一个 RPC 方法
type RPCMethod struct {
	Name       string // 方法名称，例如 SayHello
	Num        int    // 一个 RPC 方法可以对应多个 HTTP 请求
	Request    string // 请求消息类型，例如 SayHelloReq
	Reply      string // 响应消息类型，例如 SayHelloResp
	InvokeType int    // 调用类型: 0-单次调用, 1-客户端流式, 2-服务端流式, 3-双向流式

	// HTTP 规则相关信息
	Path         string // 请求路径
	Method       string // HTTP 方法
	Body         string
	ResponseBody string

	CustomKind string
	Selector   string

	IsWebSocket bool
	// 如果 Selector 是 [ctx], 则 IsPassGinContext 为 true
	// 如果 Selector 是 [no_bind], 则 IsPassGinContext 和 IsIgnoreShouldBind 都为 true
	IsPassGinContext bool
	// 如果 Selector 是 [no_bind], 则忽略 c.ShouldBindXXX，必须在 RPC 方法中手动调用 c.ShouldBindXXX()
	IsIgnoreShouldBind bool

	RequestImportPkgName string // 例如空或 userV1
	ReplyImportPkgName   string // 例如空或 userV1

	ProtoSelfPkgPath string              // 例如 "module/api/user/v1"
	ImportPkgPaths   map[string]struct{} // 排除 ProtoSelfPkgPath
}

// HandlerName 返回 gin 处理器名称
func (m *RPCMethod) HandlerName() string {
	return fmt.Sprintf("%s_%d", m.Name, m.Num)
}

// HasPathParams 判断路径是否包含路由参数
func (m *RPCMethod) HasPathParams() bool {
	paths := strings.Split(m.Path, "/")
	for _, p := range paths {
		if len(p) > 0 && (p[0] == '{' && p[len(p)-1] == '}' || p[0] == ':') {
			return true
		}
	}
	return false
}

// parseVariable 解析变量字符串
// 参数:
//   - str: string, 变量字符串
//
// 返回值:
//   - prefixStr: string, 前缀字符串
//   - isPassGinContext: bool, 是否传递 gin.Context
//   - isIgnoreShouldBind: bool, 是否忽略 ShouldBindXXX
func parseVariable(str string) (prefixStr string, isPassGinContext bool, isIgnoreShouldBind bool) {
	str = strings.ReplaceAll(str, " ", "")
	startIdx := strings.Index(str, "[")
	endIdx := strings.LastIndex(str, "]")
	if startIdx != -1 && endIdx != -1 {
		options := str[startIdx+1 : endIdx]
		ss := strings.Split(options, ",")
		for _, s := range ss {
			if s == "ctx" {
				isPassGinContext = true
			}
			if s == "no_bind" {
				isIgnoreShouldBind = true
				isPassGinContext = true // 必须传递 gin.Context
			}
		}
		prefixStr = str[:startIdx]
	} else {
		prefixStr = str
	}

	return prefixStr, isPassGinContext, isIgnoreShouldBind
}

// checkCustomKind 检查并设置自定义类型
func (m *RPCMethod) checkCustomKind() {
	if m.CustomKind == "" {
		return
	}

	customKindStr, isPassGinContext, isIgnoreShouldBind := parseVariable(m.CustomKind)
	m.IsPassGinContext = isPassGinContext
	m.IsIgnoreShouldBind = isIgnoreShouldBind

	switch customKindStr {
	case "get":
		m.Method = http.MethodGet
	case "post":
		m.Method = http.MethodPost
	case "put":
		m.Method = http.MethodPut
	case "delete":
		m.Method = http.MethodDelete
	case "patch":
		m.Method = http.MethodPatch
	case "options":
		m.Method = http.MethodOptions
	case "head":
		m.Method = http.MethodHead
	case "trace":
		m.Method = http.MethodTrace
	case "connect":
		m.Method = http.MethodConnect
	case "websocket":
		m.IsWebSocket = true
		m.Method = http.MethodGet
		m.IsPassGinContext = true
		m.IsIgnoreShouldBind = true
	default:
		m.Method = http.MethodPost
	}
}

// checkSelector 检查并设置选择器
func (m *RPCMethod) checkSelector() {
	if m.Selector == "" {
		return
	}
	_, isPassGinContext, isIgnoreShouldBind := parseVariable(m.Selector)
	m.IsPassGinContext = isPassGinContext
	m.IsIgnoreShouldBind = isIgnoreShouldBind
}

// InitPathParams 将路径参数转换为 Gin 支持的格式
func (m *RPCMethod) InitPathParams() {
	paths := strings.Split(m.Path, "/")
	for i, p := range paths {
		if len(p) > 0 && (p[0] == '{' && p[len(p)-1] == '}') {
			paths[i] = ":" + p[1:len(p)-1]
		}
	}
	m.Path = strings.Join(paths, "/")
}
