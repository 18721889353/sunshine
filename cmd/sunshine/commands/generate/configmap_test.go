package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// TestCopyConfigGenerator_generateCode 完整端到端测试：执行 ConfigMap 代码生成并复制到 testdata 供查看
func TestCopyConfigGenerator_generateCode(t *testing.T) {
	// 先生成到临时目录，再复制到 testdata 供查看
	tmpOut := filepath.Join(os.TempDir(), "sunshine-test", "configmap-gen")
	if err := os.RemoveAll(tmpOut); err != nil {
		t.Fatalf("清理旧输出目录失败: %v", err)
	}

	// 目标目录：测试文件同级目录下的 testdata
	testDataDir := filepath.Join("testdata", "configmap-gen")
	if err := os.RemoveAll(testDataDir); err != nil {
		t.Fatalf("清理旧 testdata 目录失败: %v", err)
	}

	// 初始化 Replacers[TplNameSunshine]（优先使用本地源码目录）
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

	// 真实案例：模拟一个服务的 YAML 配置内容
	configContent := `    server:
      host: 0.0.0.0
      port: 8080
    database:
      driver: mysql
      dsn: root:123456@tcp(192.168.1.1:3306)/test_db
    redis:
      addr: 127.0.0.1:6379
      db: 0
`

	gen := &copyConfigGenerator{
		serverName:  "user-service",
		projectName: "my-project",
		content:     configContent,
		outPath:     tmpOut,
	}

	// 执行代码生成
	outPath, err := gen.generateCode()
	if err != nil {
		t.Fatalf("generateCode() 失败: %v", err)
	}
	t.Logf("生成输出目录: %s", outPath)

	// 验证生成的 .yml 文件存在（期待生成 configmap 文件）
	var generatedFile string
	filepath.Walk(outPath, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && strings.HasSuffix(info.Name(), "-configmap.yml") {
			generatedFile = path
		}
		return nil
	})

	if generatedFile == "" {
		t.Fatal("未找到生成的 -configmap.yml 文件")
	}
	t.Logf("生成的文件: %s", generatedFile)

	// 复制到 testdata 目录供查看
	relPath, _ := filepath.Rel(tmpOut, generatedFile)
	targetFile := filepath.Join(testDataDir, relPath)
	if err := os.MkdirAll(filepath.Dir(targetFile), 0755); err != nil {
		t.Fatalf("创建目标目录失败: %v", err)
	}
	copyFile(t, generatedFile, targetFile)
	t.Logf("已复制到: %s", targetFile)

	// 验证文件内容包含预期的替换结果
	data, err := os.ReadFile(generatedFile)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	content := string(data)

	// 验证服务名替换（kebab-case）
	if !strings.Contains(content, "user-service") {
		t.Errorf("生成文件应包含服务名 %q", "user-service")
	}
	// 验证项目名替换
	if !strings.Contains(content, "my-project") {
		t.Errorf("生成文件应包含项目名 %q", "my-project")
	}
	// 验证 ConfigMap 中嵌入的配置内容
	if !strings.Contains(content, "host: 0.0.0.0") {
		t.Error("生成文件应包含嵌入的配置内容 host: 0.0.0.0")
	}
	if !strings.Contains(content, "driver: mysql") {
		t.Error("生成文件应包含嵌入的配置内容 driver: mysql")
	}
	// 验证源文件占位符已被替换（不应保留原始模板名）
	if strings.Contains(content, "serverNameExample") {
		t.Error("生成文件不应包含原始模板名 serverNameExample")
	}
	if strings.Contains(content, "project-name-example") {
		t.Error("生成文件不应包含原始占位符 project-name-example")
	}
	// 验证配置标记已被替换
	if strings.Contains(content, "# todo generate server configuration code here") {
		t.Error("生成文件不应包含配置标记占位符")
	}
}
