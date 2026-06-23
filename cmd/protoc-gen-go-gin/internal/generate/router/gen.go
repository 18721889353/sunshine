// Package router 是用于生成 Gin 路由代码的包。
package router

import (
	"bytes"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/18721889353/sunshine/cmd/protoc-gen-go-gin/internal/parse"
)

// GenerateFiles 生成 Gin 路由代码。
func GenerateFiles(file *protogen.File) []byte {
	// 如果文件中没有服务定义，则返回空字节切片
	if len(file.Services) == 0 {
		return nil
	}

	// 解析 HTTP 服务定义
	pss := parse.Services(file)
	// 生成 Gin 路由文件内容
	return genGinRouterFile(pss, string(file.GoPackageName))
}

// genGinRouterFile 生成 Gin 路由文件内容。
func genGinRouterFile(services parse.HTTPPbServices, goPackageName string) []byte {
	// 检查是否有 WebSocket 方法
	hasWebSocket := false
	for _, service := range services {
		for _, method := range service.Methods {
			if method.IsWebSocket {
				hasWebSocket = true
				break
			}
		}
		if hasWebSocket {
			break
		}
	}

	// 创建导入包结构体
	pkg := &importPkg{
		PackageName:  goPackageName,                 // 包名
		PackagePaths: services.MergeImportPkgPath(), // 合并导入路径
		HasWebSocket: hasWebSocket,                  // 是否包含 WebSocket 方法
	}
	// 执行导入包模板，生成导入包部分的代码
	content := pkg.execute()

	// 遍历每个服务，生成路由字段代码
	for _, service := range services {
		rf := &ginRouterFields{service}            // 创建 ginRouterFields 结构体
		content = append(content, rf.execute()...) // 执行模板，生成路由字段代码并追加到内容中
	}
	return content
}

// importPkg 用于生成导入包部分的结构体。
type importPkg struct {
	PackageName  string // 包名
	PackagePaths string // 导入路径
	HasWebSocket bool   // 是否包含 WebSocket 方法
}

// execute 执行导入包模板，生成对应的代码。
func (f *importPkg) execute() []byte {
	buf := new(bytes.Buffer) // 创建缓冲区
	// 执行模板，将生成的代码写入缓冲区
	if err := importPkgTmpl.Execute(buf, f); err != nil {
		panic(err) // 如果执行模板出错，则抛出 panic
	}
	return buf.Bytes() // 返回生成的代码字节切片
}

// ginRouterFields 用于生成 Gin 路由字段的结构体。
type ginRouterFields struct {
	*parse.HTTPPbService // 嵌入 HTTPPbService 结构体
}

// execute 执行 Gin 路由字段模板，生成对应的代码。
func (f *ginRouterFields) execute() []byte {
	buf := new(bytes.Buffer) // 创建缓冲区
	// 执行模板，将生成的代码写入缓冲区
	if err := ginRouterTmpl.Execute(buf, f); err != nil {
		panic(err) // 如果执行模板出错，则抛出 panic
	}
	return buf.Bytes() // 返回生成的代码字节切片
}
