package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// TestGrpcConnectionGenerator_generateCode 端到端测试：生成 gRPC 连接代码
func TestGrpcConnectionGenerator_generateCode(t *testing.T) {
	tmpOut := filepath.Join(os.TempDir(), "sunshine-test", "rpc-conn-gen")
	os.RemoveAll(tmpOut)
	testDataDir := filepath.Join("testdata", "rpc-conn-gen")
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

	gen := &grpcConnectionGenerator{
		moduleName:     "github.com/myorg/myproject",
		grpcName:       "user",
		serverName:     "user-service",
		outPath:        tmpOut,
		suitedMonoRepo: false,
	}

	outPath, err := gen.generateCode()
	if err != nil {
		t.Fatalf("generateCode() 失败: %v", err)
	}
	t.Logf("生成输出目录: %s", outPath)

	var gf string
	filepath.Walk(outPath, func(path string, info os.FileInfo, _ error) error {
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

	if !strings.Contains(content, "package rpcclient") {
		t.Error("生成文件应包含 package rpcclient")
	}
	if !strings.Contains(content, "github.com/myorg/myproject") {
		t.Errorf("生成文件应包含模块名 %q", "github.com/myorg/myproject")
	}
	if strings.Contains(content, "serverNameExample") {
		t.Error("生成文件不应包含原始模板名 serverNameExample")
	}

	t.Log("--- 所有生成的文件 ---")
	filepath.Walk(outPath, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			rel, _ := filepath.Rel(outPath, path)
			dst := filepath.Join(testDataDir, rel)
			os.MkdirAll(filepath.Dir(dst), 0755)
			copyFile(t, path, dst)
			t.Logf("  %s", rel)
		}
		return nil
	})
}
