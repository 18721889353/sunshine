// Package gozip provides ZIP file compression and decompression utilities.
package gozip

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeka/zip"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("gozip.request_id", reqID)
		}
	}
	return attribute.String("gozip.request_id", "")
}

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
func ZipFilesFromPaths(ctx context.Context, sourceFiles []string, destPath string, password string) (*ZipFileResult, error) {
	return ZipFilesFromPathsWithOptions(ctx, sourceFiles, destPath, &ZipOptions{
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
func ZipFilesFromPathsWithOptions(ctx context.Context, sourceFiles []string, destPath string, options *ZipOptions) (*ZipFileResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gozip")
	spanName := fmt.Sprintf("gozip.zip_files_from_paths_with_options")
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("gozip.operation", "zip_files_from_paths_with_options"),
		attribute.Int("gozip.source_file_count", len(sourceFiles)),
		attribute.String("gozip.dest_path", destPath),
		attribute.Bool("gozip.is_encrypted", options != nil && options.Password != ""),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	if len(sourceFiles) == 0 {
		err := fmt.Errorf("源文件列表为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
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

	logger.InfoWithCtx(ctx, "开始ZIP压缩",
		logger.String("dest_path", destPath),
		logger.Int("file_count", len(sourceFiles)),
		logger.Bool("is_encrypted", options.Password != ""))

	// 创建ZIP文件
	zipFile, err := os.Create(destPath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("创建ZIP文件失败: %w", err)
	}
	defer func() {
		if closeErr := zipFile.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "关闭ZIP文件失败", logger.Err(closeErr))
		}
	}()

	// 创建ZIP写入器
	zipWriter := zip.NewWriter(zipFile)
	defer func() {
		if closeErr := zipWriter.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "关闭ZIP写入器失败", logger.Err(closeErr))
		}
	}()

	fileCount := 0
	totalSize := int64(0)

	// 遍历所有源文件，添加到ZIP中
	for _, sourceFile := range sourceFiles {
		// 验证源文件是否存在
		if _, statErr := os.Stat(sourceFile); statErr != nil {
			err = fmt.Errorf("源文件不存在 [%s]: %w", sourceFile, statErr)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}

		// 读取源文件内容
		fileData, readErr := os.ReadFile(sourceFile)
		if readErr != nil {
			err = fmt.Errorf("读取源文件失败 [%s]: %w", sourceFile, readErr)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}

		// 获取文件名(不含路径)
		fileName := filepath.Base(sourceFile)

		// 创建统一的 FileHeader
		header := &zip.FileHeader{
			Name:   fileName,
			Method: zip.Deflate,
		}
		// 关键点：设置UTF-8标志，解决中文文件名乱码问题
		header.Flags |= 0x800

		var writer io.Writer
		var writeErr error

		if options.Password != "" {
			// 加密模式：在 header 上设置密码和加密算法，并使用 CreateHeader 统一写入
			header.SetPassword(options.Password)
			// 注意：yeka/zip 内部使用的是方法 SetEncryptionMethod，而非结构体字段
			header.SetEncryptionMethod(getEncryptionMethod(options.Encryption))

			writer, writeErr = zipWriter.CreateHeader(header)
			if writeErr != nil {
				err = fmt.Errorf("创建加密ZIP条目失败 [%s]: %w", fileName, writeErr)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}
		} else {
			// 非加密模式
			writer, writeErr = zipWriter.CreateHeader(header)
			if writeErr != nil {
				err = fmt.Errorf("创建ZIP条目失败 [%s]: %w", fileName, writeErr)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}
		}

		// 写入文件内容
		if _, writeErr := io.Copy(writer, strings.NewReader(string(fileData))); writeErr != nil {
			err = fmt.Errorf("写入ZIP文件内容失败 [%s]: %w", fileName, writeErr)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, writeErr
		}

		fileCount++
		totalSize += int64(len(fileData))

		logger.DebugWithCtx(context.Background(), "文件已添加到ZIP",
			logger.String("file_name", fileName),
			logger.Int64("file_size", int64(len(fileData))))
	}

	// 刷新并关闭ZIP写入器
	if flushErr := zipWriter.Flush(); flushErr != nil {
		err = fmt.Errorf("刷新ZIP文件失败: %w", flushErr)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// 获取压缩文件大小
	fileInfo, statErr := zipFile.Stat()
	if statErr != nil {
		logger.WarnWithCtx(ctx, "获取压缩文件大小失败", logger.Err(statErr))
	}

	result := &ZipFileResult{
		Path:        destPath,
		Size:        fileInfo.Size(),
		FileCount:   fileCount,
		IsEncrypted: options.Password != "",
	}

	logger.InfoWithCtx(ctx, "ZIP压缩完成",
		logger.String("dest_path", destPath),
		logger.Int("file_count", fileCount),
		logger.Int64("total_original_size", totalSize),
		logger.Int64("compressed_size", fileInfo.Size()),
		logger.Float64("compression_ratio", calculateCompressionRatio(totalSize, fileInfo.Size())))

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("gozip.file_count", fileCount),
		attribute.Int64("gozip.total_original_size", totalSize),
		attribute.Int64("gozip.compressed_size", fileInfo.Size()),
		attribute.Float64("gozip.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "zip completed successfully")

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
func ZipDirectory(ctx context.Context, sourceDir string, destPath string, password string) (*ZipFileResult, error) {
	return ZipDirectoryWithOptions(ctx, sourceDir, destPath, &ZipOptions{
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
func ZipDirectoryWithOptions(ctx context.Context, sourceDir string, destPath string, options *ZipOptions) (*ZipFileResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gozip")
	spanName := fmt.Sprintf("gozip.zip_directory_with_options")
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("gozip.operation", "zip_directory_with_options"),
		attribute.String("gozip.source_dir", sourceDir),
		attribute.String("gozip.dest_path", destPath),
		attribute.Bool("gozip.is_encrypted", options != nil && options.Password != ""),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	if sourceDir == "" {
		err := fmt.Errorf("源目录路径为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// 验证目录是否存在
	dirInfo, statErr := os.Stat(sourceDir)
	if statErr != nil {
		err := fmt.Errorf("源目录不存在: %w", statErr)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if !dirInfo.IsDir() {
		err := fmt.Errorf("指定路径不是目录: %s", sourceDir)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("遍历目录失败: %w", err)
	}

	if len(filePaths) == 0 {
		emptyDirErr := fmt.Errorf("目录为空: %s", sourceDir)
		span.RecordError(emptyDirErr)
		span.SetStatus(codes.Error, emptyDirErr.Error())
		return nil, emptyDirErr
	}

	// 使用已有的文件列表压缩方法
	result, err := ZipFilesFromPathsWithOptions(ctx, filePaths, destPath, options)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("gozip.file_count", result.FileCount),
		attribute.Int64("gozip.compressed_size", result.Size),
		attribute.Float64("gozip.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "directory zip completed successfully")

	return result, nil
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
