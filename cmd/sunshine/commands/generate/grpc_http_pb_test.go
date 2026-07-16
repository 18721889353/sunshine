package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// TestHttpAndGRPCPbGenerator_generateCode 端到端测试：根据 greeter.proto 生成 gRPC+HTTP 混合服务代码
func TestHttpAndGRPCPbGenerator_generateCode(t *testing.T) {
	tmpOut := filepath.Join(os.TempDir(), "sunshine-test", "grpc-http-pb-gen")
	os.RemoveAll(tmpOut)
	testDataDir := filepath.Join("testdata", "grpc-http-pb-gen")
	os.RemoveAll(testDataDir)

	// 初始化 Replacers[TplNameSunshine]
	sourceDir := detectLocalSunshineSource()
	if sourceDir == "" {
		sourceDir = SunshineDir
	}
	if _, ok := Replacers[TplNameSunshine]; !ok {
		r, err := replacer.New(sourceDir)
		if err != nil {
			t.Fatalf("初始化 replacer 失败: %v", err)
		}
		Replacers[TplNameSunshine] = r
		t.Cleanup(func() { delete(Replacers, TplNameSunshine) })
	}

	// 获取 greeter.proto 的绝对路径
	protoFile := getGreeterProtoPath(t)

	gen := &httpAndGRPCPbGenerator{
		moduleName:     "github.com/myorg/greeter",
		serverName:     "greeter",
		projectName:    "my-project",
		protobufFile:   protoFile,
		repoAddr:       "192.168.1.1:5000/myorg",
		outPath:        tmpOut,
		suitedMonoRepo: false,
	}

	err := gen.generateCode()
	if err != nil {
		t.Fatalf("generateCode() 失败: %v", err)
	}
	t.Log("生成成功")

	// 查找生成的 Go 文件
	var gf string
	filepath.Walk(tmpOut, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			gf = path
		}
		return nil
	})
	if gf == "" {
		t.Fatal("未找到生成的 .go 文件")
	}
	t.Logf("生成的文件: %s", gf)

	// 复制到 testdata 目录供查看
	relPath, _ := filepath.Rel(tmpOut, gf)
	targetFile := filepath.Join(testDataDir, relPath)
	os.MkdirAll(filepath.Dir(targetFile), 0755)
	copyFile(t, gf, targetFile)
	t.Logf("已复制到: %s", targetFile)

	data, err := os.ReadFile(gf)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	content := string(data)

	// 验证模块名替换
	if !strings.Contains(content, "github.com/myorg/greeter") {
		t.Log("注意：生成文件可能不含完整模块导入路径，检查实际内容:")
		t.Log(content[:min(len(content), 200)])
	}
	// 验证占位符已被替换
	if strings.Contains(content, "serverNameExample") {
		t.Error("生成文件不应包含原始模板名 serverNameExample")
	}

	// 列出所有生成的文件
	t.Log("--- 所有生成的文件 ---")
	filepath.Walk(tmpOut, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			rel, _ := filepath.Rel(tmpOut, path)
			dst := filepath.Join(testDataDir, rel)
			os.MkdirAll(filepath.Dir(dst), 0755)
			copyFile(t, path, dst)
			t.Logf("  %s", rel)
		}
		return nil
	})
}

// getGreeterProtoPath 获取 greeter.proto 文件的绝对路径
func getGreeterProtoPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取当前目录失败: %v", err)
	}
	protoFile := filepath.Join(wd, "greeter.proto")
	if _, err := os.Stat(protoFile); err != nil {
		t.Fatalf("proto 文件不存在: %v", err)
	}
	return protoFile
}
