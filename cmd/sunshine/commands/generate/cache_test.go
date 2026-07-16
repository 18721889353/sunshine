package generate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// TestStringCacheGenerator_generateCode 完整端到端测试：执行代码生成并验证生成的文件
func TestStringCacheGenerator_generateCode(t *testing.T) {
	// 先生成到临时目录（replacer 禁止写入源码目录），再复制到 testdata 供查看
	tmpOut := filepath.Join(os.TempDir(), "sunshine-test", "cache-gen")
	if err := os.RemoveAll(tmpOut); err != nil {
		t.Fatalf("清理旧输出目录失败: %v", err)
	}

	// 目标目录：测试文件同级目录下的 testdata
	testDataDir := filepath.Join("testdata", "cache-gen")
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

	gen := &stringCacheGenerator{
		moduleName: "testapp",
		cacheName:  "UserToken",
		prefixKey:  "user:token",
		keyName:    "uid",
		keyType:    "uint64",
		valueName:  "token",
		valueType:  "string",
		outPath:    tmpOut,
	}

	// 执行代码生成
	outPath, err := gen.generateCode()
	if err != nil {
		t.Fatalf("generateCode() 失败: %v", err)
	}
	t.Logf("生成输出目录: %s", outPath)

	// 验证生成的 .go 文件存在
	var generatedFile string
	filepath.Walk(outPath, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			generatedFile = path
		}
		return nil
	})

	if generatedFile == "" {
		t.Fatal("未找到生成的 .go 文件")
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

	// 验证模块名替换（影响导入路径）
	if !strings.Contains(content, "testapp") {
		t.Errorf("生成文件应包含模块名 %q", "testapp")
	}
	// 验证缓存名称替换为指定方法名
	if !strings.Contains(content, "UserToken") {
		t.Errorf("生成文件应包含缓存名 %q", "UserToken")
	}
	// 验证值类型替换为 string
	if !strings.Contains(content, "string") {
		t.Error("生成文件应包含值类型 string")
	}
	// 验证前缀键替换
	if !strings.Contains(content, "user:token") {
		t.Errorf("生成文件应包含前缀键 %q", "user:token")
	}
	// 验证源文件占位符已被替换（不应保留原始模板名）
	if strings.Contains(content, "cacheNameExample") {
		t.Error("生成文件不应包含原始模板文件名 cacheNameExample")
	}
	// 验证删除标记模板代码已被清理
	if strings.Contains(content, "delete the templates code") {
		t.Error("生成文件不应包含删除模板标记")
	}
}

// copyFile 复制文件到目标路径
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("打开源文件失败: %v", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("创建目标文件失败: %v", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("复制文件失败: %v", err)
	}
}
