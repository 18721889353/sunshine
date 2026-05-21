package gozip

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/yeka/zip"
)

// ZipCompressionLevel ZIP压缩级别常量
const (
	ZipDefaultCompression = 0  // 默认压缩级别
	ZipBestCompression    = -3 // 最佳压缩（最慢）
	ZipBestSpeed          = -1 // 最快速度（最低压缩率）
	ZipNoCompression      = 0  // 不压缩，仅存储
)

// ZipEncryptionType ZIP加密类型
type ZipEncryptionType int

const (
	// ZipStandardEncryption 标准ZIP加密（兼容性最好，安全性较低）
	ZipStandardEncryption ZipEncryptionType = iota
	// ZipAES128Encryption AES-128加密
	ZipAES128Encryption
	// ZipAES192Encryption AES-192加密
	ZipAES192Encryption
	// ZipAES256Encryption AES-256加密（推荐，最安全）
	ZipAES256Encryption
)

// ZipOptions ZIP压缩选项
type ZipOptions struct {
	Password       string            // 密码（可选，为空则不加密）
	Encryption     ZipEncryptionType // 加密类型（默认AES-256）
	Compression    int               // 压缩级别（默认DefaultCompression）
	IncludeBaseDir bool              // 是否包含基础目录（仅对目录压缩有效）
}

// ZipFileResult ZIP压缩结果
type ZipFileResult struct {
	Path        string // 压缩文件路径
	Size        int64  // 压缩文件大小（字节）
	FileCount   int    // 包含的文件数量
	IsEncrypted bool   // 是否加密
}

// ZipFilesFromPaths 从文件路径列表创建ZIP压缩文件（带密码保护）
//
// 参数:
//   - sourceFiles: 源文件路径列表
//   - destPath: 目标ZIP文件路径（如果为空，自动生成临时文件）
//   - password: 压缩密码（可选，为空则不加密）
//
// 返回:
//   - *ZipFileResult: 压缩结果（包含路径、大小、文件数等）
//   - error: 错误信息
//
// 使用示例:
//
//	files := []string{"/path/to/file1.xlsx", "/path/to/file2.xlsx"}
//	result, err := tools.ZipFilesFromPaths(files, "", "password123")
//	if err != nil {
//	    log.Printf("压缩失败: %v", err)
//	}
//	fmt.Printf("压缩文件: %s, 大小: %d bytes\n", result.Path, result.Size)
func ZipFilesFromPaths(sourceFiles []string, destPath string, password string) (*ZipFileResult, error) {
	return ZipFilesFromPathsWithOptions(sourceFiles, destPath, &ZipOptions{
		Password:   password,
		Encryption: ZipAES256Encryption,
	})
}

// ZipFilesFromPathsWithOptions 从文件路径列表创建ZIP压缩文件（支持自定义选项）
//
// 参数:
//   - sourceFiles: 源文件路径列表
//   - destPath: 目标ZIP文件路径（如果为空，自动生成临时文件）
//   - options: 压缩选项（密码、加密类型、压缩级别等）
//
// 返回:
//   - *ZipFileResult: 压缩结果
//   - error: 错误信息
//
// 使用示例:
//
//	files := []string{"/path/to/file1.xlsx", "/path/to/file2.xlsx"}
//	options := &tools.ZipOptions{
//	    Password:   "password123",
//	    Encryption: tools.ZipAES256Encryption,
//	    Compression: tools.ZipBestCompression,
//	}
//	result, err := tools.ZipFilesFromPathsWithOptions(files, "", options)
func ZipFilesFromPathsWithOptions(sourceFiles []string, destPath string, options *ZipOptions) (*ZipFileResult, error) {
	if len(sourceFiles) == 0 {
		return nil, fmt.Errorf("源文件列表为空")
	}

	// 设置默认选项
	if options == nil {
		options = &ZipOptions{
			Encryption:  ZipAES256Encryption,
			Compression: ZipDefaultCompression,
		}
	}
	if options.Compression == 0 {
		options.Compression = ZipDefaultCompression
	}

	// 生成目标路径（如果未提供）
	if destPath == "" {
		destPath = filepath.Join(os.TempDir(), fmt.Sprintf("compress_%d.zip", getCurrentTimestamp()))
	}

	logger.InfoWithCtx(context.Background(), "开始ZIP压缩",
		logger.String("dest_path", destPath),
		logger.Int("file_count", len(sourceFiles)),
		logger.Bool("is_encrypted", options.Password != ""))

	// 创建ZIP文件
	zipFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("创建ZIP文件失败: %w", err)
	}
	defer func() {
		if closeErr := zipFile.Close(); closeErr != nil {
			logger.WarnWithCtx(context.Background(), "关闭ZIP文件失败", logger.Err(closeErr))
		}
	}()

	// 创建ZIP写入器
	zipWriter := zip.NewWriter(zipFile)
	defer func() {
		if closeErr := zipWriter.Close(); closeErr != nil {
			logger.WarnWithCtx(context.Background(), "关闭ZIP写入器失败", logger.Err(closeErr))
		}
	}()

	fileCount := 0
	totalSize := int64(0)

	// 遍历所有源文件，添加到ZIP中
	for _, sourceFile := range sourceFiles {
		// 验证源文件是否存在
		if _, statErr := os.Stat(sourceFile); statErr != nil {
			return nil, fmt.Errorf("源文件不存在 [%s]: %w", sourceFile, statErr)
		}

		// 读取源文件内容
		fileData, readErr := os.ReadFile(sourceFile)
		if readErr != nil {
			return nil, fmt.Errorf("读取源文件失败 [%s]: %w", sourceFile, readErr)
		}

		// 获取文件名(不含路径)
		fileName := filepath.Base(sourceFile)

		// 创建ZIP条目（可选加密）
		var writer io.Writer
		var writeErr error

		if options.Password != "" {
			// 加密模式
			encryptionMethod := getEncryptionMethod(options.Encryption)
			writer, writeErr = zipWriter.Encrypt(fileName, options.Password, encryptionMethod)
			if writeErr != nil {
				return nil, fmt.Errorf("创建加密ZIP条目失败 [%s]: %w", fileName, writeErr)
			}
		} else {
			// 非加密模式
			header := &zip.FileHeader{
				Name:   fileName,
				Method: zip.Deflate,
			}
			writer, writeErr = zipWriter.CreateHeader(header)
			if writeErr != nil {
				return nil, fmt.Errorf("创建ZIP条目失败 [%s]: %w", fileName, writeErr)
			}
		}

		// 写入文件内容
		if _, writeErr := io.Copy(writer, strings.NewReader(string(fileData))); writeErr != nil {
			return nil, fmt.Errorf("写入ZIP文件内容失败 [%s]: %w", fileName, writeErr)
		}

		fileCount++
		totalSize += int64(len(fileData))

		logger.DebugWithCtx(context.Background(), "文件已添加到ZIP",
			logger.String("file_name", fileName),
			logger.Int64("file_size", int64(len(fileData))))
	}

	// 刷新并关闭ZIP写入器
	if flushErr := zipWriter.Flush(); flushErr != nil {
		return nil, fmt.Errorf("刷新ZIP文件失败: %w", flushErr)
	}

	// 获取压缩文件大小
	fileInfo, statErr := zipFile.Stat()
	if statErr != nil {
		logger.WarnWithCtx(context.Background(), "获取压缩文件大小失败", logger.Err(statErr))
	}

	result := &ZipFileResult{
		Path:        destPath,
		Size:        fileInfo.Size(),
		FileCount:   fileCount,
		IsEncrypted: options.Password != "",
	}

	logger.InfoWithCtx(context.Background(), "ZIP压缩完成",
		logger.String("dest_path", destPath),
		logger.Int("file_count", fileCount),
		logger.Int64("total_original_size", totalSize),
		logger.Int64("compressed_size", fileInfo.Size()),
		logger.Float64("compression_ratio", calculateCompressionRatio(totalSize, fileInfo.Size())))

	return result, nil
}

// ZipDirectory 压缩整个目录（递归包含子目录）
//
// 参数:
//   - sourceDir: 源目录路径
//   - destPath: 目标ZIP文件路径（如果为空，自动生成临时文件）
//   - password: 压缩密码（可选）
//
// 返回:
//   - *ZipFileResult: 压缩结果
//   - error: 错误信息
//
// 使用示例:
//
//	result, err := tools.ZipDirectory("/path/to/dir", "", "password123")
func ZipDirectory(sourceDir string, destPath string, password string) (*ZipFileResult, error) {
	return ZipDirectoryWithOptions(sourceDir, destPath, &ZipOptions{
		Password:   password,
		Encryption: ZipAES256Encryption,
	})
}

// ZipDirectoryWithOptions 压缩整个目录（支持自定义选项）
//
// 参数:
//   - sourceDir: 源目录路径
//   - destPath: 目标ZIP文件路径
//   - options: 压缩选项
//
// 返回:
//   - *ZipFileResult: 压缩结果
//   - error: 错误信息
func ZipDirectoryWithOptions(sourceDir string, destPath string, options *ZipOptions) (*ZipFileResult, error) {
	if sourceDir == "" {
		return nil, fmt.Errorf("源目录路径为空")
	}

	// 验证目录是否存在
	dirInfo, statErr := os.Stat(sourceDir)
	if statErr != nil {
		return nil, fmt.Errorf("源目录不存在: %w", statErr)
	}
	if !dirInfo.IsDir() {
		return nil, fmt.Errorf("指定路径不是目录: %s", sourceDir)
	}

	// 收集所有文件路径
	var filePaths []string
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			filePaths = append(filePaths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历目录失败: %w", err)
	}

	if len(filePaths) == 0 {
		return nil, fmt.Errorf("目录为空: %s", sourceDir)
	}

	// 使用已有的文件列表压缩方法
	return ZipFilesFromPathsWithOptions(filePaths, destPath, options)
}

// ==================== 辅助函数 ====================

// getEncryptionMethod 将加密类型转换为zip库的加密方法
func getEncryptionMethod(encType ZipEncryptionType) zip.EncryptionMethod {
	switch encType {
	case ZipStandardEncryption:
		return zip.StandardEncryption
	case ZipAES128Encryption:
		return zip.AES128Encryption
	case ZipAES192Encryption:
		return zip.AES192Encryption
	case ZipAES256Encryption:
		return zip.AES256Encryption
	default:
		return zip.AES256Encryption // 默认使用AES-256
	}
}

// getCurrentTimestamp 获取当前时间戳
func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}

// calculateCompressionRatio 计算压缩率（百分比）
func calculateCompressionRatio(originalSize, compressedSize int64) float64 {
	if originalSize == 0 {
		return 0
	}
	return float64(compressedSize) / float64(originalSize) * 100
}
