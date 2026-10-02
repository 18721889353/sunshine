// Package gozip 封装 ZIP 压缩工具，支持文件列表压缩、目录递归压缩、
// ZipCrypto/AES 加密以及中文文件名的 UTF-8 编码处理。
//
// 核心功能：
//   - 文件列表压缩：ZipFilesFromPaths / ZipFilesFromPathsWithOptions 把若干文件写入同一个 ZIP，
//     destPath 为空时自动创建唯一临时文件（并发安全）。
//   - 目录递归压缩：ZipDirectory / ZipDirectoryWithOptions 保留子目录相对路径结构，
//     支持 IncludeBaseDir 将源目录名作为顶层前缀（实现见 dir.go）。
//   - 加密：支持标准 ZipCrypto 与 AES-128/192/256，密码为空则不加密。
//   - 压缩方法：ZipNoCompression 写 zip.Store，其余级别写 zip.Deflate；
//     底层库 yeka/zip 未实现 deflate 级别调节，级别常量不改变压缩强度。
//   - 链路追踪：每个导出 API 创建 OTel Span，并从 ctx 提取 request_id 写入 gozip.request_id 属性。
package gozip

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// failSpan 把错误记录到 span 并返回同一错误，调用方直接 `return nil, failSpan(span, err)`。
func failSpan(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// Zip 压缩级别常量。
//
// 底层库 yeka/zip 未实现 deflate 级别调节（writer.go:17 `TODO(adg): support specifying deflate level`），
// 因此级别常量只决定「是否压缩」，不改变压缩强度：
//   - ZipNoCompression：条目以 zip.Store 写入（不压缩）
//   - 其余常量：条目以 zip.Deflate 写入，ZipBestSpeed / ZipBestCompression 与默认效果一致
const (
	ZipDefaultCompression = 0  // 默认压缩级别（zip.Deflate）
	ZipBestCompression    = -2 // 最佳压缩（底层不支持级别调节，等同默认）
	ZipBestSpeed          = -1 // 最快速度（同上）
	ZipNoCompression      = -3 // 不压缩，仅存储（zip.Store）
)

// ZipEncryptionType ZIP 加密类型。
type ZipEncryptionType int

const (
	// ZipDefaultEncryption 未设置时的默认加密类型，归一化后等价于 ZipAES256Encryption。
	// 把零值定义为默认而非 ZipCrypto，是为了让 ZipOptions 的零值与便捷函数 ZipFilesFromPaths
	// 的默认行为保持一致（避免“只加了个 Compression 选项却把加密从 AES-256 降级到 ZipCrypto”）。
	ZipDefaultEncryption ZipEncryptionType = iota
	// ZipStandardEncryption 标准ZIP加密（兼容性最好，安全性较低）
	ZipStandardEncryption
	// ZipAES128Encryption AES-128加密
	ZipAES128Encryption
	// ZipAES192Encryption AES-192加密
	ZipAES192Encryption
	// ZipAES256Encryption AES-256加密（推荐，最安全）
	ZipAES256Encryption
)

// ZipOptions ZIP 压缩选项。
type ZipOptions struct {
	Password       string            // 密码（可选，为空则不加密）
	Encryption     ZipEncryptionType // 加密类型；零值 ZipDefaultEncryption 等价 ZipAES256Encryption，需 ZipCrypto 请显式指定 ZipStandardEncryption
	Compression    int               // 压缩级别；ZipNoCompression 写 zip.Store，其余写 zip.Deflate（零值即默认，非法值返回错误）
	IncludeBaseDir bool              // 是否包含基础目录（仅对目录压缩有效，见 ZipDirectoryWithOptions）
	TempDir        string            // 自动生成临时文件的目录（仅 destPath 为空时生效；空则用系统默认临时目录；目录须已存在）
}

// ZipFileResult ZIP 压缩结果。
type ZipFileResult struct {
	Path        string // 压缩文件路径
	Size        int64  // 压缩文件大小（字节）
	FileCount   int    // 包含的文件数量
	IsEncrypted bool   // 是否加密
}

// zipEntry 描述一个待写入 ZIP 的条目：磁盘源路径与 ZIP 内条目名（正斜杠分隔）。
type zipEntry struct {
	srcPath   string // 磁盘上的源文件路径
	entryName string // ZIP 内条目名
}

// ZipFilesFromPaths 从文件路径列表创建 ZIP 压缩文件（带密码保护）。
//
// 参数:
//   - sourceFiles: 源文件路径列表
//   - destPath: 目标 ZIP 文件路径（如果为空，自动创建唯一临时文件）
//   - password: 压缩密码（可选，为空则不加密）
//
// 返回:
//   - *ZipFileResult: 压缩结果（包含路径、大小、文件数等）
//   - error: 错误信息
//
// 使用示例:
//
//	files := []string{"/path/to/file1.xlsx", "/path/to/file2.xlsx"}
//	result, err := gozip.ZipFilesFromPaths(context.Background(), files, "", "password123")
//	if err != nil {
//	    log.Printf("压缩失败: %v", err)
//	    return
//	}
//	fmt.Printf("压缩文件: %s, 大小: %d bytes\n", result.Path, result.Size)
func ZipFilesFromPaths(ctx context.Context, sourceFiles []string, destPath string, password string) (*ZipFileResult, error) {
	return ZipFilesFromPathsWithOptions(ctx, sourceFiles, destPath, &ZipOptions{
		Password:   password,
		Encryption: ZipAES256Encryption,
	})
}

// ZipFilesFromPathsWithOptions 从文件路径列表创建 ZIP 压缩文件（支持自定义选项）。
//
// 参数:
//   - sourceFiles: 源文件路径列表
//   - destPath: 目标 ZIP 文件路径（如果为空，自动创建唯一临时文件）
//   - options: 压缩选项（密码、加密类型、压缩级别等），nil 时使用默认值
//
// 返回:
//   - *ZipFileResult: 压缩结果
//   - error: 错误信息
//
// 使用示例:
//
//	files := []string{"/path/to/file1.xlsx", "/path/to/file2.xlsx"}
//	options := &gozip.ZipOptions{
//	    Password:    "password123",
//	    Encryption:  gozip.ZipAES256Encryption,
//	    Compression: gozip.ZipBestCompression,
//	}
//	result, err := gozip.ZipFilesFromPathsWithOptions(context.Background(), files, "", options)
func ZipFilesFromPathsWithOptions(ctx context.Context, sourceFiles []string, destPath string, options *ZipOptions) (*ZipFileResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gozip")
	ctx, span := tracer.Start(ctx, "gozip.zip_files_from_paths_with_options", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	startTime := time.Now()

	// 归一化并校验选项：非法枚举值在这里 fail loud，错误记录进 span
	opts, optErr := normalizeOptions(options)
	if optErr != nil {
		return nil, failSpan(span, optErr)
	}

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("gozip.operation", "zip_files_from_paths_with_options"),
		attribute.Int("gozip.source_file_count", len(sourceFiles)),
		attribute.String("gozip.dest_path", destPath),
		attribute.Bool("gozip.is_encrypted", opts.Password != ""),
		requestIDAttr(ctx),
	)

	if len(sourceFiles) == 0 {
		return nil, failSpan(span, fmt.Errorf("源文件列表为空"))
	}

	// 文件列表压缩：条目名取文件基名（保持与历史行为一致）
	entries := make([]zipEntry, 0, len(sourceFiles))
	for _, sourceFile := range sourceFiles {
		entries = append(entries, zipEntry{srcPath: sourceFile, entryName: filepath.Base(sourceFile)})
	}

	result, err := writeZipEntries(ctx, span, entries, destPath, &opts, startTime)
	if err != nil {
		return nil, failSpan(span, err)
	}
	return result, nil
}

// writeZipEntries 把条目列表写入目标 ZIP，是文件列表压缩与目录压缩共用的核心实现。
// 负责：创建目标文件（含临时文件回填）、写条目、写 central directory、关闭资源、
// 失败时清理半成品文件。调用方负责创建 span 并在失败时记录错误。
func writeZipEntries(ctx context.Context, span trace.Span, entries []zipEntry, destPath string, options *ZipOptions, startTime time.Time) (*ZipFileResult, error) {
	// 生成目标路径（未提供时创建唯一临时文件，避免旧实现秒级时间戳在并发下互相覆盖）
	destPath, zipFile, err := createDestFile(destPath, options.TempDir)
	if err != nil {
		return nil, err
	}

	zipWriter := zip.NewWriter(zipFile)
	var writerClosed, fileClosed, completed bool
	defer func() {
		// 兜底关闭：显式 Close 已完成时跳过，避免 "zip: writer closed twice"
		if !writerClosed {
			if closeErr := zipWriter.Close(); closeErr != nil {
				logger.WarnWithCtx(ctx, "关闭ZIP写入器失败", logger.Err(closeErr))
			}
		}
		if !fileClosed {
			if closeErr := zipFile.Close(); closeErr != nil {
				logger.WarnWithCtx(ctx, "关闭ZIP文件失败", logger.Err(closeErr))
			}
		}
		// 压缩失败时清理半成品，避免残留不完整的 ZIP 文件
		if !completed {
			if rmErr := os.Remove(destPath); rmErr != nil && !os.IsNotExist(rmErr) {
				logger.WarnWithCtx(ctx, "清理半成品ZIP文件失败",
					logger.String("dest_path", destPath),
					logger.Err(rmErr))
			}
		}
	}()

	// 开始日志降为 Debug：大批量压缩时热路径不应刷 INFO；完成日志仍为 INFO 承载体积/耗时统计
	logger.DebugWithCtx(ctx, "开始ZIP压缩",
		logger.String("dest_path", destPath),
		logger.Int("file_count", len(entries)),
		logger.Bool("is_encrypted", options.Password != ""))

	fileCount := 0
	totalSize := int64(0)
	for _, entry := range entries {
		written, writeErr := writeZipEntry(ctx, zipWriter, entry, options)
		if writeErr != nil {
			return nil, fmt.Errorf("写入ZIP条目 [%s] 失败: %w", entry.entryName, writeErr)
		}
		fileCount++
		totalSize += written
	}

	// 写入 central directory：必须显式 Close 并检查错误，Flush 无法发现写目录阶段的失败
	writerClosed = true
	if closeErr := zipWriter.Close(); closeErr != nil {
		return nil, fmt.Errorf("关闭ZIP写入器失败: %w", closeErr)
	}

	// 获取压缩后大小（Stat 失败只降级为 0 并警告，不能 panic 中断调用方）
	compressedSize := int64(0)
	if info, statErr := zipFile.Stat(); statErr != nil {
		logger.WarnWithCtx(ctx, "获取压缩文件大小失败", logger.Err(statErr))
	} else {
		compressedSize = info.Size()
	}

	// 关闭底层文件：调用前置标志，Close 失败也不再让 defer 二次关闭
	fileClosed = true
	if closeErr := zipFile.Close(); closeErr != nil {
		return nil, fmt.Errorf("关闭ZIP文件失败: %w", closeErr)
	}
	completed = true

	result := &ZipFileResult{
		Path:        destPath,
		Size:        compressedSize,
		FileCount:   fileCount,
		IsEncrypted: options.Password != "",
	}

	logger.InfoWithCtx(ctx, "ZIP压缩完成",
		logger.String("dest_path", destPath),
		logger.Int("file_count", fileCount),
		logger.Int64("total_original_size", totalSize),
		logger.Int64("compressed_size", compressedSize),
		logger.Float64("compression_ratio", calculateCompressedPercent(totalSize, compressedSize)))

	// 设置成功的追踪属性
	span.SetAttributes(
		attribute.Int("gozip.file_count", fileCount),
		attribute.Int64("gozip.total_original_size", totalSize),
		attribute.Int64("gozip.compressed_size", compressedSize),
		attribute.Float64("gozip.duration_ms", float64(time.Since(startTime).Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "zip completed successfully")

	return result, nil
}

// writeZipEntry 将单个源文件流式写入 ZIP 条目，返回写入的原始字节数。
// 使用 os.Open + io.Copy 而非整文件读入内存，避免大文件场景下峰值内存翻倍。
func writeZipEntry(ctx context.Context, zipWriter *zip.Writer, entry zipEntry, options *ZipOptions) (int64, error) {
	src, err := os.Open(entry.srcPath)
	if err != nil {
		return 0, fmt.Errorf("打开源文件失败: %w", err)
	}
	defer func() {
		if closeErr := src.Close(); closeErr != nil {
			logger.WarnWithCtx(ctx, "关闭源文件失败",
				logger.String("path", entry.srcPath),
				logger.Err(closeErr))
		}
	}()

	header := &zip.FileHeader{
		Name:   entry.entryName,
		Method: methodForCompression(options.Compression),
	}
	// 关键点：设置 UTF-8 标志，解决中文文件名乱码问题
	header.Flags |= 0x800
	if options.Password != "" {
		header.SetPassword(options.Password)
		// yeka/zip 内部使用 SetEncryptionMethod 方法，而非结构体字段
		header.SetEncryptionMethod(getEncryptionMethod(options.Encryption))
	}

	entryWriter, err := zipWriter.CreateHeader(header)
	if err != nil {
		return 0, fmt.Errorf("创建ZIP条目失败: %w", err)
	}

	written, err := io.Copy(entryWriter, src)
	if err != nil {
		return 0, fmt.Errorf("写入ZIP内容失败: %w", err)
	}

	logger.DebugWithCtx(ctx, "文件已添加到ZIP",
		logger.String("file_name", entry.entryName),
		logger.Int64("file_size", written))

	return written, nil
}

// createDestFile 创建目标 ZIP 文件：destPath 为空时用 os.CreateTemp 在 tempDir（可空，空则系统默认
// 临时目录）下生成唯一文件，复用其句柄避免二次打开。
func createDestFile(destPath, tempDir string) (string, *os.File, error) {
	if destPath == "" {
		zipFile, err := os.CreateTemp(tempDir, "gozip-*.zip")
		if err != nil {
			return "", nil, fmt.Errorf("创建临时ZIP文件失败: %w", err)
		}
		return zipFile.Name(), zipFile, nil
	}
	zipFile, err := os.Create(destPath)
	if err != nil {
		return destPath, nil, fmt.Errorf("创建ZIP文件失败: %w", err)
	}
	return destPath, zipFile, nil
}

// normalizeOptions 归一化并校验压缩选项：返回副本避免修改调用方传入的结构体；
// 零值 Encryption（ZipDefaultEncryption）归一为 AES-256，与便捷函数 ZipFilesFromPaths 一致；
// 未知加密类型或非法压缩级别直接返回错误（fail loud），不静默回退。
func normalizeOptions(options *ZipOptions) (ZipOptions, error) {
	var opts ZipOptions
	if options == nil {
		opts = ZipOptions{
			Encryption:  ZipAES256Encryption,
			Compression: ZipDefaultCompression,
		}
	} else {
		opts = *options
	}

	switch opts.Encryption {
	case ZipDefaultEncryption:
		// 零值 = 默认，归一为 AES-256，保证两套 API 加密强度一致
		opts.Encryption = ZipAES256Encryption
	case ZipStandardEncryption, ZipAES128Encryption, ZipAES192Encryption, ZipAES256Encryption:
		// 显式指定，保持原值
	default:
		return ZipOptions{}, fmt.Errorf("未知的加密类型: %d", opts.Encryption)
	}

	switch opts.Compression {
	case ZipDefaultCompression, ZipBestSpeed, ZipBestCompression, ZipNoCompression:
		// 合法压缩级别
	default:
		return ZipOptions{}, fmt.Errorf("未知的压缩级别: %d", opts.Compression)
	}

	return opts, nil
}

// ==================== 辅助函数 ====================

// getEncryptionMethod 将加密类型转换为 zip 库的加密方法。
// 合法枚举值在 normalizeOptions 已校验；default 分支仅作为防御层，未知值回退 AES-256。
func getEncryptionMethod(encType ZipEncryptionType) zip.EncryptionMethod {
	switch encType {
	case ZipStandardEncryption:
		return zip.StandardEncryption
	case ZipAES128Encryption:
		return zip.AES128Encryption
	case ZipAES192Encryption:
		return zip.AES192Encryption
	case ZipAES256Encryption, ZipDefaultEncryption:
		return zip.AES256Encryption
	default:
		return zip.AES256Encryption
	}
}

// methodForCompression 将压缩级别映射为 ZIP 条目压缩方法。
// yeka/zip 未实现 deflate 级别（writer.go:17 TODO），故只区分存储与 Deflate。
func methodForCompression(level int) uint16 {
	if level == ZipNoCompression {
		return zip.Store
	}
	return zip.Deflate
}

// calculateCompressedPercent 计算压缩后大小占原始大小的百分比（压缩后 / 原始 × 100，值越低表示压得越小）。
// 日志字段名保持 compression_ratio 不变，避免破坏下游日志解析。
func calculateCompressedPercent(originalSize, compressedSize int64) float64 {
	if originalSize == 0 {
		return 0
	}
	return float64(compressedSize) / float64(originalSize) * 100
}
