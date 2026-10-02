package gozip

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeka/zip"
)

// createTestFiles 创建多个测试文件，返回它们的绝对路径。
func createTestFiles(t testing.TB, dir string, count int) []string {
	t.Helper()
	files := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		filename := fmt.Sprintf("test_file_%d.txt", i)
		content := fmt.Sprintf("This is test file content number %d", i)
		files = append(files, createTestFile(t, dir, filename, content))
	}
	return files
}

// createTestFile 创建单个测试文件并返回其绝对路径。
func createTestFile(t testing.TB, dir, name, content string) string {
	t.Helper()
	filePath := filepath.Join(dir, name)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("创建测试文件失败 [%s]: %v", name, err)
	}
	return filePath
}

// openZip 打开已生成的 ZIP 包用于断言，测试结束时自动关闭。
func openZip(t *testing.T, path string) *zip.ReadCloser {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("无法打开生成的ZIP包 [%s]: %v", path, err)
	}
	t.Cleanup(func() {
		if closeErr := reader.Close(); closeErr != nil {
			t.Logf("关闭ZIP读取器失败: %v", closeErr)
		}
	})
	return reader
}

// zipEntryNames 提取 ZIP 包内全部条目名，便于断言结构。
func zipEntryNames(t testing.TB, reader *zip.ReadCloser) []string {
	t.Helper()
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	return names
}

// hasWinZipAESExtra 判断条目是否携带 WinZip AES extra field（header id 0x9901）。
// 这是区分 AES 加密与 ZipCrypto 的可靠依据：yeka/zip 写入时把 Method 改为 99，
// 但读取时会经该 extra field 把 Method 还原成原始压缩方法（reader.go:313），
// 因此不能用 Method 判断加密算法；ZipCrypto 条目则没有此 extra field。
func hasWinZipAESExtra(file *zip.File) bool {
	for i := 0; i+4 <= len(file.Extra); {
		id := binary.LittleEndian.Uint16(file.Extra[i:])
		size := binary.LittleEndian.Uint16(file.Extra[i+2:])
		if id == 0x9901 {
			return true
		}
		i += 4 + int(size)
	}
	return false
}
