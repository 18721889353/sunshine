package gozip

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// 本文件是压缩性能的回归基线：本地磁盘临时目录，不含网络 RTT，
// 读数量的是本包自身的压缩开销。运行方式见 README「性能基线与模糊测试」。

// BenchmarkZipFilesFromPathsNoPassword 测量无密码（Deflate）压缩 10 个小文件的基线。
func BenchmarkZipFilesFromPathsNoPassword(b *testing.B) {
	tempDir := b.TempDir()
	files := createTestFiles(b, tempDir, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		destPath := filepath.Join(tempDir, fmt.Sprintf("bench_plain_%d.zip", i))
		if _, err := ZipFilesFromPaths(context.Background(), files, destPath, ""); err != nil {
			b.Fatalf("压缩失败: %v", err)
		}
	}
}

// BenchmarkZipFilesFromPathsWithAES256 测量 AES-256 加密压缩 10 个小文件的基线，
// 与无密码基准的差值即加密开销。
func BenchmarkZipFilesFromPathsWithAES256(b *testing.B) {
	tempDir := b.TempDir()
	files := createTestFiles(b, tempDir, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		destPath := filepath.Join(tempDir, fmt.Sprintf("bench_aes_%d.zip", i))
		if _, err := ZipFilesFromPaths(context.Background(), files, destPath, "bench-password-123"); err != nil {
			b.Fatalf("压缩失败: %v", err)
		}
	}
}

// BenchmarkZipDirectory 测量目录递归压缩（含遍历与相对路径计算）的基线。
func BenchmarkZipDirectory(b *testing.B) {
	sourceDir := newTestSourceDir(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		destPath := filepath.Join(b.TempDir(), fmt.Sprintf("bench_dir_%d.zip", i))
		if _, err := ZipDirectory(context.Background(), sourceDir, destPath, ""); err != nil {
			b.Fatalf("压缩目录失败: %v", err)
		}
	}
}
