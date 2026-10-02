// 目录递归压缩：遍历源目录并保留子目录相对路径结构，核心条目收集见 collectDirEntries。

package gozip

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ZipDirectory 压缩整个目录（递归包含子目录，条目保留相对路径结构）。
//
// 参数:
//   - sourceDir: 源目录路径
//   - destPath: 目标 ZIP 文件路径（如果为空，自动创建唯一临时文件）
//   - password: 压缩密码（可选）
//
// 返回:
//   - *ZipFileResult: 压缩结果
//   - error: 错误信息
//
// 使用示例:
//
//	result, err := gozip.ZipDirectory(context.Background(), "/path/to/dir", "", "password123")
func ZipDirectory(ctx context.Context, sourceDir string, destPath string, password string) (*ZipFileResult, error) {
	return ZipDirectoryWithOptions(ctx, sourceDir, destPath, &ZipOptions{
		Password:   password,
		Encryption: ZipAES256Encryption,
	})
}

// ZipDirectoryWithOptions 压缩整个目录（支持自定义选项）。
//
// 参数:
//   - sourceDir: 源目录路径
//   - destPath: 目标 ZIP 文件路径
//   - options: 压缩选项；IncludeBaseDir 为 true 时条目以源目录名作为顶层前缀
//
// 返回:
//   - *ZipFileResult: 压缩结果
//   - error: 错误信息
//
// 条目命名规则：ZIP 内条目为相对 sourceDir 的正斜杠路径（如 subdir/file.txt），
// 因此不同子目录下的同名文件不会互相覆盖。
func ZipDirectoryWithOptions(ctx context.Context, sourceDir string, destPath string, options *ZipOptions) (*ZipFileResult, error) {
	// 链路追踪
	tracer := otel.Tracer("gozip")
	ctx, span := tracer.Start(ctx, "gozip.zip_directory_with_options", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	startTime := time.Now()

	// 归一化并校验选项：非法枚举值在这里 fail loud，错误记录进 span
	opts, optErr := normalizeOptions(options)
	if optErr != nil {
		return nil, failSpan(span, optErr)
	}

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("gozip.operation", "zip_directory_with_options"),
		attribute.String("gozip.source_dir", sourceDir),
		attribute.String("gozip.dest_path", destPath),
		attribute.Bool("gozip.is_encrypted", opts.Password != ""),
		requestIDAttr(ctx),
	)

	if sourceDir == "" {
		return nil, failSpan(span, fmt.Errorf("源目录路径为空"))
	}

	// 验证目录是否存在
	dirInfo, statErr := os.Stat(sourceDir)
	if statErr != nil {
		return nil, failSpan(span, fmt.Errorf("源目录不存在: %w", statErr))
	}
	if !dirInfo.IsDir() {
		return nil, failSpan(span, fmt.Errorf("指定路径不是目录: %s", sourceDir))
	}

	// 收集条目列表（保留相对路径结构）
	entries, err := collectDirEntries(sourceDir, opts.IncludeBaseDir)
	if err != nil {
		return nil, failSpan(span, err)
	}
	if len(entries) == 0 {
		return nil, failSpan(span, fmt.Errorf("目录为空: %s", sourceDir))
	}

	// 成功属性与 Span 状态由 writeZipEntries 统一设置（与 ZipFilesFromPathsWithOptions 保持一致，
	// 不再在此重复 SetAttributes/SetStatus，避免埋点语义被两处维护）
	result, err := writeZipEntries(ctx, span, entries, destPath, &opts, startTime)
	if err != nil {
		return nil, failSpan(span, err)
	}
	return result, nil
}

// collectDirEntries 遍历目录，生成保留相对路径结构的 ZIP 条目列表。
// 条目名使用正斜杠分隔（ZIP 规范要求）；includeBaseDir 为 true 时以源目录名作为顶层前缀。
func collectDirEntries(sourceDir string, includeBaseDir bool) ([]zipEntry, error) {
	var entries []zipEntry
	walkErr := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(sourceDir, path)
		if relErr != nil {
			return fmt.Errorf("计算相对路径失败 [%s]: %w", path, relErr)
		}
		entryName := rel
		if includeBaseDir {
			entryName = filepath.Join(filepath.Base(sourceDir), rel)
		}
		entries = append(entries, zipEntry{
			srcPath:   path,
			entryName: filepath.ToSlash(entryName),
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("遍历目录失败: %w", walkErr)
	}
	return entries, nil
}
