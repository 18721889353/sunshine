package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// TestRpcGwPbGenerator_generateCode 端到端测试：根据 greeter.proto 生成 gRPC Gateway 服务代码
func TestRpcGwPbGenerator_generateCode(t *testing.T) {
	tmpOut := filepath.Join(os.TempDir(), "sunshine-test", "rpc-gw-pb-gen")
	os.RemoveAll(tmpOut)
	testDataDir := filepath.Join("testdata", "rpc-gw-pb-gen")
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

	protoFile := getGreeterProtoPath(t)

	gen := &rpcGwPbGenerator{
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

	if strings.Contains(content, "serverNameExample") {
		t.Error("生成文件不应包含原始模板名 serverNameExample")
	}

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
