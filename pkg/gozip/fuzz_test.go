package gozip

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeka/zip"
)

// isValidTestFileName 判断文件名能否在本机文件系统落地（Windows/类 Unix 通用的保守子集）。
// 不满足时 fuzz 用例 Skip：这是环境限制，不是被测代码的问题。
func isValidTestFileName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if len(name) > 200 {
		return false
	}
	// 路径分隔符会把文件名变成子路径，条目名断言也就失去意义
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	// Windows 非法字符与控制字符
	if strings.ContainsAny(name, `<>:"|?*`) {
		return false
	}
	for _, r := range name {
		if r < 0x20 {
			return false
		}
	}
	// Windows 不允许以空格或点结尾
	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return false
	}
	// Windows 设备保留名
	base := strings.ToLower(name)
	if idx := strings.IndexByte(base, '.'); idx > 0 {
		base = base[:idx]
	}
	switch base {
	case "con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return false
	}
	return true
}

// FuzzZipRoundTrip 验证压缩-解压往返不变量：任意文件名与内容压缩后，
// 条目名、UTF-8 标志位与文件内容逐字节保持一致，且全过程不 panic。
func FuzzZipRoundTrip(f *testing.F) {
	f.Add("测试文件.txt", []byte("hello sunshine"), "")
	f.Add("한국어_조선말.xlsx", []byte{0x00, 0x01, 0xff, 0xfe}, "secret")
	f.Add("ascii_name.txt", []byte{}, "")
	f.Add("file with spaces.txt", []byte("content with spaces"), "p@ssw0rd")

	f.Fuzz(func(t *testing.T, name string, content []byte, password string) {
		if !isValidTestFileName(name) {
			t.Skipf("文件名不适合本机文件系统: %q", name)
		}
		sourcePath := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(sourcePath, content, 0644); err != nil {
			t.Skipf("无法创建测试文件: %v", err)
		}

		destPath := filepath.Join(t.TempDir(), "fuzz.zip")
		result, err := ZipFilesFromPaths(context.Background(), []string{sourcePath}, destPath, password)
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if result.FileCount != 1 {
			t.Fatalf("期望文件数 1, 实际 %d", result.FileCount)
		}
		if wantEncrypted := password != ""; result.IsEncrypted != wantEncrypted {
			t.Fatalf("加密标记不匹配，期望 %v, 实际 %v", wantEncrypted, result.IsEncrypted)
		}

		reader, err := zip.OpenReader(destPath)
		if err != nil {
			t.Fatalf("无法打开生成的ZIP包: %v", err)
		}
		defer func() {
			if closeErr := reader.Close(); closeErr != nil {
				t.Errorf("关闭ZIP读取器失败: %v", closeErr)
			}
		}()

		if len(reader.File) != 1 {
			t.Fatalf("期望 1 个条目, 实际 %d", len(reader.File))
		}
		entry := reader.File[0]

		// 不变量一：条目名与源文件名逐字节一致
		if entry.Name != name {
			t.Fatalf("条目名不一致，期望 %q, 实际 %q", name, entry.Name)
		}
		// 不变量二：UTF-8 标志位始终保留（中文文件名不乱码的前提）
		if entry.Flags&0x800 == 0 {
			t.Fatalf("条目 [%s] 缺少 UTF-8 (0x800) 标志位", entry.Name)
		}

		if password != "" {
			entry.SetPassword(password)
		}
		rc, openErr := entry.Open()
		if openErr != nil {
			t.Fatalf("打开条目失败 [%s]: %v", entry.Name, openErr)
		}
		got, readErr := io.ReadAll(rc)
		if closeErr := rc.Close(); closeErr != nil {
			t.Errorf("关闭条目流失败 [%s]: %v", entry.Name, closeErr)
		}
		if readErr != nil {
			t.Fatalf("读取条目内容失败 [%s]: %v", entry.Name, readErr)
		}
		// 不变量三：内容往返后逐字节一致
		if !bytes.Equal(got, content) {
			t.Fatalf("内容不一致，期望 %d 字节, 实际 %d 字节", len(content), len(got))
		}
	})
}
