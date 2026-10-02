package gozip

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yeka/zip"
)

// TestZipFilesFromPathsChineseFilenames 验证中文及亚洲字符文件名压缩后不乱码。
// 通过加密压缩并重新打开压缩包读取，模拟 Windows/Linux 解压软件的行为，
// 从底层断言文件名的 UTF-8 标志位与内容都被正确处理。
func TestZipFilesFromPathsChineseFilenames(t *testing.T) {
	tempDir := t.TempDir()

	// 准备复杂的中文及亚洲字符文件名
	chineseNames := []string{
		"1779763686_财务报表_数据统计.xlsx",
		"1779763687_用户反馈数据汇总表2026.xlsx",
		"한국어_조선말_测试.xlsx", // 混合亚洲字符测试
	}

	var srcFiles []string
	for _, name := range chineseNames {
		content := fmt.Sprintf("模拟加密Excel文件内容: %s", name)
		srcFiles = append(srcFiles, createTestFile(t, tempDir, name, content))
	}

	destZipPath := filepath.Join(tempDir, "chinese_encrypted_test.zip")
	password := "secure_password_123"

	result, err := ZipFilesFromPaths(context.Background(), srcFiles, destZipPath, password)
	if err != nil {
		t.Fatalf("中文文件加密压缩失败: %v", err)
	}
	if !result.IsEncrypted {
		t.Fatal("压缩包应当是加密状态")
	}

	// 核心验证：模拟解压软件重新打开压缩包，校验文件名与内容
	reader := openZip(t, destZipPath)
	if len(reader.File) != len(chineseNames) {
		t.Errorf("压缩包内的文件数量不符，期望 %d, 实际 %d", len(chineseNames), len(reader.File))
	}

	expectedMap := make(map[string]bool, len(chineseNames))
	for _, name := range chineseNames {
		expectedMap[name] = true
	}

	for _, file := range reader.File {
		// 第 11 位（0x800）为 1 代表 UTF-8 编码标志被成功写入
		if file.Flags&0x800 == 0 {
			t.Errorf("文件 [%s] 的头部缺少 UTF-8 (0x800) 标志位，Windows 解压会出现乱码", file.Name)
		}
		if !expectedMap[file.Name] {
			t.Errorf("乱码或不匹配：解压出的文件名 [%s] 不在预期列表中", file.Name)
			continue
		}

		// 验证加密内容可正常读取（确认 CreateHeader 未损坏数据流）
		if file.IsEncrypted() {
			file.SetPassword(password)
		}
		rc, openErr := file.Open()
		if openErr != nil {
			t.Errorf("无法读取文件流 [%s]: %v", file.Name, openErr)
			continue
		}
		if _, readErr := io.ReadAll(rc); readErr != nil {
			t.Errorf("读取文件内容失败 [%s]: %v", file.Name, readErr)
		}
		if closeErr := rc.Close(); closeErr != nil {
			t.Errorf("关闭文件流失败 [%s]: %v", file.Name, closeErr)
		}
	}
}

// TestZipFilesFromPaths 验证从文件路径列表创建 ZIP 的各种场景。
func TestZipFilesFromPaths(t *testing.T) {
	tempDir := t.TempDir()
	testFiles := createTestFiles(t, tempDir, 3)

	t.Run("带密码压缩", func(t *testing.T) {
		destPath := filepath.Join(tempDir, "test_with_password.zip")
		result, err := ZipFilesFromPaths(context.Background(), testFiles, destPath, "password123")
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if result == nil {
			t.Fatal("结果不应为nil")
		}
		if result.Path != destPath {
			t.Errorf("期望路径 %s, 实际 %s", destPath, result.Path)
		}
		if result.FileCount != 3 {
			t.Errorf("期望文件数 3, 实际 %d", result.FileCount)
		}
		if !result.IsEncrypted {
			t.Error("应该已加密")
		}
		if result.Size <= 0 {
			t.Error("文件大小应大于0")
		}
		if _, statErr := os.Stat(destPath); statErr != nil {
			t.Errorf("压缩文件不存在: %v", statErr)
		}
	})

	t.Run("不带密码压缩", func(t *testing.T) {
		destPath := filepath.Join(tempDir, "test_without_password.zip")
		result, err := ZipFilesFromPaths(context.Background(), testFiles, destPath, "")
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if result.IsEncrypted {
			t.Error("不应该加密")
		}
	})

	t.Run("空文件列表", func(t *testing.T) {
		if _, err := ZipFilesFromPaths(context.Background(), []string{}, "", "password"); err == nil {
			t.Error("空文件列表应该返回错误")
		}
	})

	t.Run("不存在的文件", func(t *testing.T) {
		if _, err := ZipFilesFromPaths(context.Background(), []string{"/nonexistent/file.txt"}, "", "password"); err == nil {
			t.Error("不存在的文件应该返回错误")
		}
	})

	t.Run("失败时不残留半成品", func(t *testing.T) {
		destPath := filepath.Join(tempDir, "test_fail_no_residual.zip")
		_, err := ZipFilesFromPaths(context.Background(), []string{"/nonexistent/file.txt"}, destPath, "")
		if err == nil {
			t.Fatal("不存在的文件应该返回错误")
		}
		if _, statErr := os.Stat(destPath); statErr == nil {
			t.Error("压缩失败后不应残留半成品文件")
		}
	})

	t.Run("自动生成目标路径", func(t *testing.T) {
		result, err := ZipFilesFromPaths(context.Background(), testFiles, "", "password123")
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if result.Path == "" {
			t.Error("自动生成的路径不应为空")
		}
		// R1-6 修复后的特征：临时文件由 os.CreateTemp("", "gozip-*.zip") 生成
		if !strings.HasPrefix(filepath.Base(result.Path), "gozip-") {
			t.Errorf("临时文件名应带 gozip- 前缀, 实际 %s", filepath.Base(result.Path))
		}
		if _, statErr := os.Stat(result.Path); statErr != nil {
			t.Errorf("自动生成的压缩文件不存在: %v", statErr)
		}
		t.Cleanup(func() {
			if rmErr := os.Remove(result.Path); rmErr != nil {
				t.Logf("清理临时压缩文件失败: %v", rmErr)
			}
		})
	})

	t.Run("大量文件压缩", func(t *testing.T) {
		manyFiles := createTestFiles(t, tempDir, 10)
		destPath := filepath.Join(tempDir, "test_many_files.zip")
		result, err := ZipFilesFromPaths(context.Background(), manyFiles, destPath, "password")
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if result.FileCount != 10 {
			t.Errorf("期望文件数 10, 实际 %d", result.FileCount)
		}
	})
}

// TestZipFilesFromPathsAutoPathUnique 验证 destPath 为空时并发调用生成的路径互不冲突。
func TestZipFilesFromPathsAutoPathUnique(t *testing.T) {
	tempDir := t.TempDir()
	testFiles := createTestFiles(t, tempDir, 1)

	const workers = 8
	paths := make([]string, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result, err := ZipFilesFromPaths(context.Background(), testFiles, "", "")
			if err != nil {
				errs[idx] = err
				return
			}
			paths[idx] = result.Path
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, workers)
	for i, path := range paths {
		if errs[i] != nil {
			t.Fatalf("第 %d 个并发压缩失败: %v", i, errs[i])
		}
		t.Cleanup(func() {
			if rmErr := os.Remove(path); rmErr != nil {
				t.Logf("清理临时压缩文件失败: %v", rmErr)
			}
		})
		if seen[path] {
			t.Errorf("并发生成的临时路径重复: %s", path)
		}
		seen[path] = true
	}
}

// TestZipFilesFromPathsWithOptions 验证自定义选项压缩。
func TestZipFilesFromPathsWithOptions(t *testing.T) {
	tempDir := t.TempDir()
	testFiles := createTestFiles(t, tempDir, 2)

	t.Run("自定义加密与压缩选项", func(t *testing.T) {
		options := &ZipOptions{
			Password:    "test123",
			Encryption:  ZipAES128Encryption,
			Compression: ZipBestCompression,
		}
		destPath := filepath.Join(tempDir, "test_custom_options.zip")
		result, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, destPath, options)
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if !result.IsEncrypted {
			t.Error("应该已加密")
		}
	})

	t.Run("空选项使用默认值", func(t *testing.T) {
		destPath := filepath.Join(tempDir, "test_nil_options.zip")
		result, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, destPath, nil)
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if result.FileCount != 2 {
			t.Errorf("期望文件数 2, 实际 %d", result.FileCount)
		}
		if result.IsEncrypted {
			t.Error("空密码不应该加密")
		}
	})

	t.Run("标准加密", func(t *testing.T) {
		options := &ZipOptions{
			Password:   "test123",
			Encryption: ZipStandardEncryption,
		}
		destPath := filepath.Join(tempDir, "test_standard_enc.zip")
		result, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, destPath, options)
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if !result.IsEncrypted {
			t.Error("应该已加密")
		}
		// 显式指定 StandardEncryption 应走 ZipCrypto：不携带 WinZip AES extra field
		for _, file := range openZip(t, destPath).File {
			if hasWinZipAESExtra(file) {
				t.Errorf("条目 [%s] 不应携带 AES extra field（应为 ZipCrypto）", file.Name)
			}
		}
	})

	t.Run("最佳压缩写Deflate", func(t *testing.T) {
		options := &ZipOptions{
			Password:    "test123",
			Compression: ZipBestCompression,
		}
		destPath := filepath.Join(tempDir, "test_best_compression.zip")
		result, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, destPath, options)
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if result.Size <= 0 {
			t.Error("文件大小应大于0")
		}
		for _, file := range openZip(t, destPath).File {
			if file.Method != zip.Deflate {
				t.Errorf("ZipBestCompression 应写 Deflate，条目 [%s] 实际 Method=%d", file.Name, file.Method)
			}
		}
	})

	t.Run("不压缩写Store", func(t *testing.T) {
		options := &ZipOptions{Compression: ZipNoCompression}
		destPath := filepath.Join(tempDir, "test_no_compression.zip")
		result, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, destPath, options)
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if result.IsEncrypted {
			t.Error("不应该加密")
		}
		for _, file := range openZip(t, destPath).File {
			if file.Method != zip.Store {
				t.Errorf("ZipNoCompression 应写 Store，条目 [%s] 实际 Method=%d", file.Name, file.Method)
			}
		}
	})

	t.Run("指定临时目录", func(t *testing.T) {
		customTempDir := t.TempDir()
		result, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, "", &ZipOptions{
			Password: "test123",
			TempDir:  customTempDir,
		})
		if err != nil {
			t.Fatalf("压缩失败: %v", err)
		}
		if filepath.Dir(result.Path) != customTempDir {
			t.Errorf("临时文件应创建在 TempDir [%s], 实际 [%s]", customTempDir, result.Path)
		}
		t.Cleanup(func() {
			if rmErr := os.Remove(result.Path); rmErr != nil {
				t.Logf("清理临时压缩文件失败: %v", rmErr)
			}
		})
	})

	t.Run("TempDir不存在返回错误", func(t *testing.T) {
		missingDir := filepath.Join(t.TempDir(), "not_exist")
		if _, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, "", &ZipOptions{
			TempDir: missingDir,
		}); err == nil {
			t.Fatal("TempDir 不存在时应返回错误")
		}
	})
}

// TestZipOptionsDefaults 验证选项零值走默认压缩路径。
func TestZipOptionsDefaults(t *testing.T) {
	tempDir := t.TempDir()
	testFiles := createTestFiles(t, tempDir, 1)

	destPath := filepath.Join(tempDir, "test_defaults.zip")
	result, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, destPath, &ZipOptions{})
	if err != nil {
		t.Fatalf("压缩失败: %v", err)
	}
	if result == nil {
		t.Fatal("结果不应为nil")
	}
	if result.FileCount != 1 {
		t.Errorf("期望文件数 1, 实际 %d", result.FileCount)
	}
}

// TestNormalizeOptions 验证选项归一化与非法枚举值校验：
// P1-1（零值加密与便捷函数同为 AES-256）、P1-2/P1-4（未知值 fail loud）的守护测试。
func TestNormalizeOptions(t *testing.T) {
	t.Run("零值加密归一为AES256", func(t *testing.T) {
		opts, err := normalizeOptions(&ZipOptions{Password: "secret"})
		if err != nil {
			t.Fatalf("零值选项不应报错: %v", err)
		}
		if opts.Encryption != ZipAES256Encryption {
			t.Errorf("零值 Encryption 应归一为 AES-256, 实际 %v", opts.Encryption)
		}
	})

	t.Run("nil选项默认AES256", func(t *testing.T) {
		opts, err := normalizeOptions(nil)
		if err != nil {
			t.Fatalf("nil 选项不应报错: %v", err)
		}
		if opts.Encryption != ZipAES256Encryption {
			t.Errorf("nil 选项 Encryption 应为 AES-256, 实际 %v", opts.Encryption)
		}
		if opts.Compression != ZipDefaultCompression {
			t.Errorf("nil 选项 Compression 应为默认, 实际 %d", opts.Compression)
		}
	})

	t.Run("显式标准加密保持不变", func(t *testing.T) {
		opts, err := normalizeOptions(&ZipOptions{Password: "secret", Encryption: ZipStandardEncryption})
		if err != nil {
			t.Fatalf("显式标准加密不应报错: %v", err)
		}
		if opts.Encryption != ZipStandardEncryption {
			t.Errorf("显式 ZipStandardEncryption 应保持, 实际 %v", opts.Encryption)
		}
	})

	t.Run("未知加密类型返回错误", func(t *testing.T) {
		if _, err := normalizeOptions(&ZipOptions{Password: "secret", Encryption: ZipEncryptionType(999)}); err == nil {
			t.Error("未知加密类型应返回错误，不应静默回退")
		}
	})

	t.Run("非法压缩级别返回错误", func(t *testing.T) {
		for _, level := range []int{-999, 42, 100} {
			if _, err := normalizeOptions(&ZipOptions{Compression: level}); err == nil {
				t.Errorf("非法压缩级别 %d 应返回错误", level)
			}
		}
	})

	t.Run("合法压缩级别透传", func(t *testing.T) {
		for _, level := range []int{ZipDefaultCompression, ZipBestSpeed, ZipBestCompression, ZipNoCompression} {
			opts, err := normalizeOptions(&ZipOptions{Compression: level})
			if err != nil {
				t.Errorf("合法压缩级别 %d 不应报错: %v", level, err)
				continue
			}
			if opts.Compression != level {
				t.Errorf("压缩级别应透传, 期望 %d, 实际 %d", level, opts.Compression)
			}
		}
	})

	t.Run("不修改调用方结构体", func(t *testing.T) {
		in := &ZipOptions{Password: "secret"} // Encryption 为零值
		if _, err := normalizeOptions(in); err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if in.Encryption != ZipDefaultEncryption {
			t.Errorf("调用方结构体不应被修改, 实际 Encryption %v", in.Encryption)
		}
	})
}

// TestZipZeroValueEncryptionMatchesConvenience 验证从便捷函数迁移到 WithOptions 时
// 加密强度不降级（P1-1 安全降级陷阱守护）：零值加密与便捷函数同为 AES-256。
func TestZipZeroValueEncryptionMatchesConvenience(t *testing.T) {
	tempDir := t.TempDir()
	testFiles := createTestFiles(t, tempDir, 1)

	// WinZip AES 加密会写入 0x9901 extra field（yeka/zip crypto.go writeWinZipExtra）；
	// ZipCrypto 则不写。注意：不能用 Method 判别——reader 读取时会把 Method 从 99
	// 还原为原始压缩方法（yeka/zip reader.go:313）。
	zeroValuePath := filepath.Join(tempDir, "zero_value_encryption.zip")
	// 典型迁移场景：从便捷函数迁到 WithOptions，只加 Compression、不指定 Encryption
	if _, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, zeroValuePath,
		&ZipOptions{Password: "secret", Compression: ZipNoCompression}); err != nil {
		t.Fatalf("零值加密压缩失败: %v", err)
	}

	conveniencePath := filepath.Join(tempDir, "convenience_encryption.zip")
	if _, err := ZipFilesFromPaths(context.Background(), testFiles, conveniencePath, "secret"); err != nil {
		t.Fatalf("便捷函数压缩失败: %v", err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"零值加密WithOptions", zeroValuePath},
		{"便捷函数", conveniencePath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archive := openZip(t, tc.path) // 由 t.Cleanup 关闭
			if len(archive.File) != 1 {
				t.Fatalf("期望 1 个条目, 实际 %d", len(archive.File))
			}
			if !hasWinZipAESExtra(archive.File[0]) {
				t.Errorf("应使用 AES-256（携带 0x9901 extra field），实际为 ZipCrypto（加密强度降级）")
			}
		})
	}
}

// TestZipFilesFromPathsWithOptionsInvalidEnum 验证非法枚举值 fail loud 且不落盘（P1-2/P1-4 守护）。
func TestZipFilesFromPathsWithOptionsInvalidEnum(t *testing.T) {
	tempDir := t.TempDir()
	testFiles := createTestFiles(t, tempDir, 1)

	tests := []struct {
		name    string
		options *ZipOptions
	}{
		{"未知加密类型", &ZipOptions{Password: "secret", Encryption: ZipEncryptionType(999)}},
		{"非法压缩级别", &ZipOptions{Compression: -999}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destPath := filepath.Join(tempDir, "invalid_enum.zip")
			if _, err := ZipFilesFromPathsWithOptions(context.Background(), testFiles, destPath, tt.options); err == nil {
				t.Fatal("非法枚举值应返回错误")
			}
			if _, statErr := os.Stat(destPath); statErr == nil {
				t.Error("校验失败时不应创建目标文件")
			}
		})
	}
}

// TestZipWithSpecialFilenames 验证特殊字符文件名可正常压缩。
func TestZipWithSpecialFilenames(t *testing.T) {
	tempDir := t.TempDir()

	specialNames := []string{
		"file with spaces.txt",
		"file-with-dashes.txt",
		"file_with_underscores.txt",
		"文件中文名称.txt",
	}

	files := make([]string, 0, len(specialNames))
	for _, name := range specialNames {
		files = append(files, createTestFile(t, tempDir, name, fmt.Sprintf("Content of %s", name)))
	}

	destPath := filepath.Join(tempDir, "test_special_names.zip")
	result, err := ZipFilesFromPaths(context.Background(), files, destPath, "password123")
	if err != nil {
		t.Fatalf("压缩失败: %v", err)
	}
	if result.FileCount != len(specialNames) {
		t.Errorf("期望文件数 %d, 实际 %d", len(specialNames), result.FileCount)
	}
}

// TestZipLargeFile 验证大文件压缩及压缩效果。
func TestZipLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过大型文件测试")
	}

	tempDir := t.TempDir()
	largeContent := strings.Repeat("This is a large file content for testing compression. ", 20000)
	largeFile := createTestFile(t, tempDir, "large_file.txt", largeContent)

	destPath := filepath.Join(tempDir, "test_large_file.zip")
	result, err := ZipFilesFromPaths(context.Background(), []string{largeFile}, destPath, "password123")
	if err != nil {
		t.Fatalf("压缩失败: %v", err)
	}
	if result.FileCount != 1 {
		t.Errorf("期望文件数 1, 实际 %d", result.FileCount)
	}

	// 高度重复的文本压缩后应显著小于原文。阈值 50% 是保守水位而非紧贴预期值：
	// 本机实测该语料压缩率为 0.31%（1,080,000 → 3,400 bytes，见下方 t.Logf），
	// 距阈值有 160 倍余量，机器/Go 版本差异不会触发误报；越阈说明压缩链路实际损坏而非环境敏感
	originalSize := int64(len(largeContent))
	compressionRatio := calculateCompressedPercent(originalSize, result.Size)
	t.Logf("原始大小: %d bytes, 压缩大小: %d bytes, 压缩率: %.2f%%",
		originalSize, result.Size, compressionRatio)
	if compressionRatio > 50 {
		t.Errorf("压缩率 %.2f%% 过高，重复文本应压缩到原文一半以下", compressionRatio)
	}
}

// TestZipEmptyPassword 验证空密码不触发加密。
func TestZipEmptyPassword(t *testing.T) {
	tempDir := t.TempDir()
	testFiles := createTestFiles(t, tempDir, 2)

	destPath := filepath.Join(tempDir, "test_empty_password.zip")
	result, err := ZipFilesFromPaths(context.Background(), testFiles, destPath, "")
	if err != nil {
		t.Fatalf("压缩失败: %v", err)
	}
	if result.IsEncrypted {
		t.Error("空密码不应该加密")
	}
}

// TestZipFilesFromPathsVerifyArchive 验证压缩产物的存在性与元数据完整性。
func TestZipFilesFromPathsVerifyArchive(t *testing.T) {
	tempDir := t.TempDir()

	file1 := createTestFile(t, tempDir, "integration_test_1.txt", "Integration test content 1")
	file2 := createTestFile(t, tempDir, "integration_test_2.txt", "Integration test content 2")

	destPath := filepath.Join(tempDir, "verify_archive.zip")
	result, err := ZipFilesFromPaths(context.Background(), []string{file1, file2}, destPath, "integration_password")
	if err != nil {
		t.Fatalf("压缩失败: %v", err)
	}

	fileInfo, statErr := os.Stat(destPath)
	if statErr != nil {
		t.Fatalf("压缩文件不存在: %v", statErr)
	}
	if fileInfo.Size() <= 0 {
		t.Error("压缩文件大小应为正数")
	}
	if result.FileCount != 2 {
		t.Errorf("期望文件数 2, 实际 %d", result.FileCount)
	}
	if !result.IsEncrypted {
		t.Error("应该已加密")
	}
	if result.Size != fileInfo.Size() {
		t.Errorf("结果记录的大小 %d 与实际文件大小 %d 不一致", result.Size, fileInfo.Size())
	}

	t.Logf("压缩成功: 路径=%s, 大小=%d bytes, 文件数=%d, 已加密=%v",
		result.Path, result.Size, result.FileCount, result.IsEncrypted)
}

// TestGetEncryptionMethod 验证加密类型到 zip 库加密方法的映射。
func TestGetEncryptionMethod(t *testing.T) {
	tests := []struct {
		name     string
		encType  ZipEncryptionType
		expected zip.EncryptionMethod
	}{
		{"标准加密", ZipStandardEncryption, zip.StandardEncryption},
		{"AES-128加密", ZipAES128Encryption, zip.AES128Encryption},
		{"AES-192加密", ZipAES192Encryption, zip.AES192Encryption},
		{"AES-256加密", ZipAES256Encryption, zip.AES256Encryption},
		{"零值默认归一AES-256", ZipDefaultEncryption, zip.AES256Encryption},
		{"未知类型防御性回退AES-256（入口已拦截）", ZipEncryptionType(999), zip.AES256Encryption},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := getEncryptionMethod(tt.encType)
			if method != tt.expected {
				t.Errorf("期望加密方法 %v, 实际 %v", tt.expected, method)
			}
		})
	}
}

// TestMethodForCompression 验证压缩级别到 ZIP 条目压缩方法的映射。
func TestMethodForCompression(t *testing.T) {
	tests := []struct {
		name     string
		level    int
		expected uint16
	}{
		{"默认级别写Deflate", ZipDefaultCompression, zip.Deflate},
		{"最佳压缩写Deflate", ZipBestCompression, zip.Deflate},
		{"最快速度写Deflate", ZipBestSpeed, zip.Deflate},
		{"不压缩写Store", ZipNoCompression, zip.Store},
		{"未知级别防御性写Deflate（入口已拦截）", 999, zip.Deflate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := methodForCompression(tt.level)
			if method != tt.expected {
				t.Errorf("期望压缩方法 %d, 实际 %d", tt.expected, method)
			}
		})
	}
}

// TestCalculateCompressedPercent 验证压缩后百分比计算。
func TestCalculateCompressedPercent(t *testing.T) {
	tests := []struct {
		name           string
		originalSize   int64
		compressedSize int64
		expected       float64
	}{
		{"正常压缩", 1000, 500, 50.0},
		{"无压缩", 1000, 1000, 100.0},
		{"高压缩", 1000, 100, 10.0},
		{"原始大小为零", 0, 100, 0.0},
		{"压缩后变大", 100, 200, 200.0},
		{"两者都为零", 0, 0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio := calculateCompressedPercent(tt.originalSize, tt.compressedSize)
			if ratio != tt.expected {
				t.Errorf("期望压缩百分比 %.2f, 实际 %.2f", tt.expected, ratio)
			}
		})
	}
}
