package generate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// mockCacheReplacer 实现 replacer.Replacer 接口的模拟对象，用于缓存代码生成器测试。
type mockCacheReplacer struct {
	replacer.Replacer
	readFileErr bool
}

func (m *mockCacheReplacer) ReadFile(_ string) ([]byte, error) {
	if m.readFileErr {
		return nil, errors.New("mock read file error")
	}
	return []byte("// <start>mock content<end>"), nil
}
func (m *mockCacheReplacer) GetSourcePath() string { return "templates/sunshine" }

// TestStringCacheGenerator_addFields 验证 addFields 方法在各种配置下生成的替换规则
func TestStringCacheGenerator_addFields(t *testing.T) {
	tests := []struct {
		name    string
		gen     *stringCacheGenerator
		wantMin int // 期望生成的替换规则数量下限
	}{
		{
			name: "基本类型（非指针）",
			gen: &stringCacheGenerator{
				moduleName: "myapp",
				cacheName:  "UserToken",
				prefixKey:  "user:token",
				keyName:    "uid",
				keyType:    "uint64",
				valueName:  "token",
				valueType:  "string",
			},
			wantMin: 10,
		},
		{
			name: "指针类型值",
			gen: &stringCacheGenerator{
				moduleName: "myapp",
				cacheName:  "UserSession",
				prefixKey:  "user:session",
				keyName:    "id",
				keyType:    "string",
				valueName:  "session",
				valueType:  "*Session",
			},
			wantMin: 12, // 指针类型会额外生成两条替换规则
		},
		{
			name: "单体仓库模式",
			gen: &stringCacheGenerator{
				moduleName:     "myapp",
				cacheName:      "OrgConfig",
				prefixKey:      "org:config",
				keyName:        "orgID",
				keyType:        "uint64",
				valueName:      "config",
				valueType:      "*Config",
				serverName:     "org-service",
				suitedMonoRepo: true,
			},
			wantMin: 14, // 指针类型 + mono-repo 额外规则
		},
		{
			name: "冒号前缀补全",
			gen: &stringCacheGenerator{
				moduleName: "test",
				cacheName:  "Token",
				prefixKey:  "token", // 末尾无冒号，应自动补全
				keyName:    "uid",
				keyType:    "string",
				valueName:  "token",
				valueType:  "string",
			},
			wantMin: 10,
		},
		{
			name: "空前缀键",
			gen: &stringCacheGenerator{
				moduleName: "test",
				cacheName:  "Data",
				prefixKey:  "",
				keyName:    "id",
				keyType:    "uint64",
				valueName:  "data",
				valueType:  "interface{}",
			},
			wantMin: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockCacheReplacer{readFileErr: false}
			fields := tt.gen.addFields(mock)

			if len(fields) < tt.wantMin {
				t.Errorf("addFields() 返回 %d 条替换规则，期望至少 %d 条", len(fields), tt.wantMin)
			}

			// 验证核心替换规则是否包含预期的占位符
			checkFieldContains(t, fields, "github.com/18721889353/sunshine/internal/database")
			checkFieldContains(t, fields, "cacheNameExample.go")
			checkFieldContains(t, fields, "NameExample")
			checkFieldContains(t, fields, "nameExample")
			checkFieldContains(t, fields, "CacheName")
			checkFieldContains(t, fields, "keyNameExample")
			checkFieldContains(t, fields, "prefixKeyExample:")
			checkFieldContains(t, fields, "keyTypeExample")
			checkFieldContains(t, fields, "valueTypeExample")
			checkFieldContains(t, fields, "valueNameExample")
		})
	}
}

// TestStringCacheGenerator_addFields_ReadFileError 验证 ReadFile 失败时不会 panic
func TestStringCacheGenerator_addFields_ReadFileError(t *testing.T) {
	gen := &stringCacheGenerator{
		moduleName: "test",
		cacheName:  "Test",
		prefixKey:  "test:",
		keyName:    "id",
		keyType:    "uint64",
		valueName:  "data",
		valueType:  "string",
	}

	mock := &mockCacheReplacer{readFileErr: true}
	fields := gen.addFields(mock)

	if len(fields) == 0 {
		t.Error("addFields() 在 ReadFile 失败时不应返回空字段列表")
	}
}

// TestStringCacheGenerator_pointerFields 验证指针类型值的特殊替换逻辑
func TestStringCacheGenerator_pointerFields(t *testing.T) {
	gen := &stringCacheGenerator{
		moduleName: "test",
		cacheName:  "User",
		prefixKey:  "user:",
		keyName:    "id",
		keyType:    "uint64",
		valueName:  "user",
		valueType:  "*User",
	}

	mock := &mockCacheReplacer{readFileErr: true}
	fields := gen.addFields(mock)

	foundNewVar := false
	foundRef := false
	for _, f := range fields {
		if f.Old == "var valueNameExample valueTypeExample" {
			foundNewVar = true
			if f.New != "user := &User{}" {
				t.Errorf("指针类型变量声明错误: got %q, want %q", f.New, "user := &User{}")
			}
		}
		if f.Old == "&valueNameExample" {
			foundRef = true
			if f.New != "user" {
				t.Errorf("指针类型取地址替换错误: got %q, want %q", f.New, "user")
			}
		}
	}
	if !foundNewVar {
		t.Error("未找到指针类型的变量声明替换规则")
	}
	if !foundRef {
		t.Error("未找到指针类型的取地址替换规则")
	}
}

// TestStringCacheGenerator_monoRepoFields 验证单体仓库模式下的额外子目录替换规则
func TestStringCacheGenerator_monoRepoFields(t *testing.T) {
	gen := &stringCacheGenerator{
		moduleName:     "myapp",
		cacheName:      "Config",
		prefixKey:      "config:",
		keyName:        "id",
		keyType:        "uint64",
		valueName:      "cfg",
		valueType:      "*Config",
		serverName:     "cfg-service",
		suitedMonoRepo: true,
	}

	mock := &mockCacheReplacer{readFileErr: true}
	fields := gen.addFields(mock)

	hasSubPathRule := false
	for _, f := range fields {
		if f.Old == `"myapp/internal/` || f.Old == `"myapp/configs` {
			hasSubPathRule = true
			break
		}
	}
	if !hasSubPathRule {
		t.Error("mono-repo 模式下未找到子路径替换规则，请检查 SubServerCodeFields 是否生效")
	}
}

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

// checkFieldContains 辅助函数：验证字段列表中是否存在包含指定 old 字符串的字段
func checkFieldContains(t *testing.T, fields []replacer.Field, old string) {
	t.Helper()
	for _, f := range fields {
		if f.Old == old {
			return
		}
	}
	t.Errorf("替换规则中未找到 Old 为 %q 的字段", old)
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
