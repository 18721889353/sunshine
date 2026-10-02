package gozip

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/yeka/zip"
)

// newTestSourceDir 构造含子目录的测试目录结构并返回根路径。
//
//	sourceDir/
//	├── file1.txt
//	├── file2.txt
//	└── subdir/
//	    └── file3.txt
func newTestSourceDir(t testing.TB) string {
	t.Helper()
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "test_source_dir")
	if err := os.MkdirAll(filepath.Join(sourceDir, "subdir"), 0755); err != nil {
		t.Fatalf("创建测试目录失败: %v", err)
	}
	createTestFile(t, sourceDir, "file1.txt", "content1")
	createTestFile(t, sourceDir, "file2.txt", "content2")
	createTestFile(t, filepath.Join(sourceDir, "subdir"), "file3.txt", "content3")
	return sourceDir
}

// TestZipDirectory 验证压缩整个目录的正向与错误场景。
func TestZipDirectory(t *testing.T) {
	tempDir := t.TempDir()
	testDir := newTestSourceDir(t)

	t.Run("压缩目录", func(t *testing.T) {
		destPath := filepath.Join(tempDir, "test_directory.zip")
		result, err := ZipDirectory(context.Background(), testDir, destPath, "password123")
		if err != nil {
			t.Fatalf("压缩目录失败: %v", err)
		}
		if result.FileCount != 3 {
			t.Errorf("期望文件数 3, 实际 %d", result.FileCount)
		}
		if !result.IsEncrypted {
			t.Error("应该已加密")
		}
	})

	t.Run("空目录返回错误", func(t *testing.T) {
		emptyDir := filepath.Join(tempDir, "empty_dir")
		if err := os.MkdirAll(emptyDir, 0755); err != nil {
			t.Fatalf("创建空目录失败: %v", err)
		}
		if _, err := ZipDirectory(context.Background(), emptyDir, "", "password"); err == nil {
			t.Error("空目录应该返回错误")
		}
	})

	t.Run("不存在的目录返回错误", func(t *testing.T) {
		if _, err := ZipDirectory(context.Background(), "/nonexistent/dir", "", "password"); err == nil {
			t.Error("不存在的目录应该返回错误")
		}
	})

	t.Run("文件路径返回错误", func(t *testing.T) {
		testFile := createTestFile(t, tempDir, "notadir.txt", "content")
		if _, err := ZipDirectory(context.Background(), testFile, "", "password"); err == nil {
			t.Error("文件路径应该返回错误")
		}
	})

	t.Run("空路径返回错误", func(t *testing.T) {
		if _, err := ZipDirectory(context.Background(), "", "", "password"); err == nil {
			t.Error("空目录路径应该返回错误")
		}
	})
}

// TestZipDirectoryWithOptions 验证带选项的目录压缩。
func TestZipDirectoryWithOptions(t *testing.T) {
	tempDir := t.TempDir()
	testDir := newTestSourceDir(t)

	t.Run("自定义加密选项", func(t *testing.T) {
		options := &ZipOptions{
			Password:   "dir_password",
			Encryption: ZipAES256Encryption,
		}
		destPath := filepath.Join(tempDir, "test_dir_custom.zip")
		result, err := ZipDirectoryWithOptions(context.Background(), testDir, destPath, options)
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if result.FileCount != 3 {
			t.Errorf("期望文件数 3, 实际 %d", result.FileCount)
		}
		if !result.IsEncrypted {
			t.Error("应该已加密")
		}
	})

	t.Run("默认不包含基础目录", func(t *testing.T) {
		destPath := filepath.Join(tempDir, "test_dir_without_base.zip")
		if _, err := ZipDirectoryWithOptions(context.Background(), testDir, destPath, nil); err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		names := zipEntryNames(t, openZip(t, destPath))
		sort.Strings(names)
		expected := []string{"file1.txt", "file2.txt", "subdir/file3.txt"}
		if len(names) != len(expected) {
			t.Fatalf("期望条目 %v, 实际 %v", expected, names)
		}
		for i, name := range names {
			if name != expected[i] {
				t.Errorf("条目名不匹配，期望 %v, 实际 %v", expected, names)
				break
			}
		}
	})

	t.Run("包含基础目录", func(t *testing.T) {
		options := &ZipOptions{IncludeBaseDir: true}
		destPath := filepath.Join(tempDir, "test_dir_with_base.zip")
		if _, err := ZipDirectoryWithOptions(context.Background(), testDir, destPath, options); err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		names := zipEntryNames(t, openZip(t, destPath))
		sort.Strings(names)
		expected := []string{"test_source_dir/file1.txt", "test_source_dir/file2.txt", "test_source_dir/subdir/file3.txt"}
		if len(names) != len(expected) {
			t.Fatalf("期望条目 %v, 实际 %v", expected, names)
		}
		for i, name := range names {
			if name != expected[i] {
				t.Errorf("条目名不匹配，期望 %v, 实际 %v", expected, names)
				break
			}
		}
	})

	t.Run("非法枚举值返回错误", func(t *testing.T) {
		destPath := filepath.Join(tempDir, "test_dir_invalid_enum.zip")
		options := &ZipOptions{Password: "dir_password", Encryption: ZipEncryptionType(999)}
		if _, err := ZipDirectoryWithOptions(context.Background(), testDir, destPath, options); err == nil {
			t.Fatal("未知加密类型应返回错误，不应静默回退")
		}
		if _, statErr := os.Stat(destPath); statErr == nil {
			t.Error("校验失败时不应创建目标文件")
		}
	})
}

// TestZipDirectoryNestedSameNames 验证不同子目录下的同名文件不会互相覆盖。
// 旧实现条目名恒取 filepath.Base，两个同名文件会在 ZIP 内产生重复条目。
func TestZipDirectoryNestedSameNames(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "dup_dir")
	nestedDir := filepath.Join(sourceDir, "nested")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	createTestFile(t, sourceDir, "data.txt", "root data")
	createTestFile(t, nestedDir, "data.txt", "nested data")

	destPath := filepath.Join(tempDir, "nested_same_names.zip")
	result, err := ZipDirectory(context.Background(), sourceDir, destPath, "")
	if err != nil {
		t.Fatalf("压缩目录失败: %v", err)
	}
	if result.FileCount != 2 {
		t.Fatalf("期望文件数 2, 实际 %d", result.FileCount)
	}

	reader := openZip(t, destPath)
	names := zipEntryNames(t, reader)
	sort.Strings(names)
	expected := []string{"data.txt", "nested/data.txt"}
	if len(names) != len(expected) {
		t.Fatalf("期望条目 %v, 实际 %v", expected, names)
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("条目名不匹配，期望 %v, 实际 %v", expected, names)
			break
		}
	}

	// 逐条读回内容，证明两个同名文件都完整保留
	wantContent := map[string]string{
		"data.txt":        "root data",
		"nested/data.txt": "nested data",
	}
	for _, file := range reader.File {
		rc, openErr := file.Open()
		if openErr != nil {
			t.Errorf("打开条目失败 [%s]: %v", file.Name, openErr)
			continue
		}
		raw, readErr := io.ReadAll(rc)
		if closeErr := rc.Close(); closeErr != nil {
			t.Errorf("关闭条目流失败 [%s]: %v", file.Name, closeErr)
		}
		if readErr != nil {
			t.Errorf("读取条目失败 [%s]: %v", file.Name, readErr)
			continue
		}
		if got := string(raw); got != wantContent[file.Name] {
			t.Errorf("条目 [%s] 内容不匹配，期望 %q, 实际 %q", file.Name, wantContent[file.Name], got)
		}
	}
}

// TestZipDirectoryDeepStructure 验证深层目录结构可完整压缩。
func TestZipDirectoryDeepStructure(t *testing.T) {
	tempDir := t.TempDir()

	deepDir := filepath.Join(tempDir, "level1", "level2", "level3", "level4")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatalf("创建深层目录失败: %v", err)
	}
	createTestFile(t, filepath.Join(tempDir, "level1"), "file1.txt", "content1")
	createTestFile(t, filepath.Join(tempDir, "level1", "level2"), "file2.txt", "content2")
	createTestFile(t, deepDir, "file3.txt", "content3")

	destPath := filepath.Join(tempDir, "test_deep_structure.zip")
	result, err := ZipDirectory(context.Background(), tempDir, destPath, "password123")
	if err != nil {
		t.Fatalf("压缩目录失败: %v", err)
	}
	if result.FileCount != 3 {
		t.Errorf("期望文件数 3, 实际 %d", result.FileCount)
	}

	for _, name := range zipEntryNames(t, openZip(t, destPath)) {
		if filepath.IsAbs(name) || filepath.IsAbs(filepath.FromSlash(name)) {
			t.Errorf("条目名不应是绝对路径: %s", name)
		}
	}
}

// TestCollectDirEntries 验证目录条目收集的相对路径与前缀逻辑。
func TestCollectDirEntries(t *testing.T) {
	sourceDir := newTestSourceDir(t)

	t.Run("相对路径正斜杠分隔", func(t *testing.T) {
		entries, err := collectDirEntries(sourceDir, false)
		if err != nil {
			t.Fatalf("收集条目失败: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("期望 3 个条目, 实际 %d", len(entries))
		}
		for _, entry := range entries {
			if entry.entryName == "" {
				t.Error("条目名不应为空")
			}
			if filepath.IsAbs(entry.entryName) {
				t.Errorf("条目名不应是绝对路径: %s", entry.entryName)
			}
			for _, r := range entry.entryName {
				if r == '\\' {
					t.Errorf("条目名必须使用正斜杠: %s", entry.entryName)
					break
				}
			}
		}
	})

	t.Run("包含基础目录前缀", func(t *testing.T) {
		entries, err := collectDirEntries(sourceDir, true)
		if err != nil {
			t.Fatalf("收集条目失败: %v", err)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.entryName, "test_source_dir/") {
				t.Errorf("条目名缺少基础目录前缀: %s", entry.entryName)
			}
		}
	})

	t.Run("不存在的目录返回错误", func(t *testing.T) {
		if _, err := collectDirEntries(filepath.Join(t.TempDir(), "missing"), false); err == nil {
			t.Error("不存在的目录应该返回错误")
		}
	})
}

// TestZipDirectoryEntryNamesMatchFiles 验证压缩条目与磁盘文件一一对应。
func TestZipDirectoryEntryNamesMatchFiles(t *testing.T) {
	sourceDir := newTestSourceDir(t)
	destPath := filepath.Join(t.TempDir(), "match.zip")

	if _, err := ZipDirectory(context.Background(), sourceDir, destPath, ""); err != nil {
		t.Fatalf("压缩目录失败: %v", err)
	}

	reader := openZip(t, destPath)
	if len(reader.File) != 3 {
		t.Fatalf("期望 3 个条目, 实际 %d", len(reader.File))
	}
	for _, file := range reader.File {
		diskPath := filepath.Join(sourceDir, filepath.FromSlash(file.Name))
		if _, statErr := os.Stat(diskPath); statErr != nil {
			t.Errorf("条目 [%s] 在磁盘上找不到对应文件: %v", file.Name, statErr)
		}
		if file.Method != zip.Deflate {
			t.Errorf("目录压缩默认应为 Deflate，条目 [%s] 实际 Method=%d", file.Name, file.Method)
		}
	}
}
