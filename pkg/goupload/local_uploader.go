package goupload

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/18721889353/sunshine/pkg/logger"
)

// LocalUploader 本地文件上传器
type LocalUploader struct {
	*BaseUploader
	rootPath string
	domain   string // 访问域名（可选）
}

// NewLocalUploader 创建本地上传器
func NewLocalUploader(config *UploaderConfig) (*LocalUploader, error) {
	if config.LocalRootPath == "" {
		return nil, fmt.Errorf("local root path cannot be empty")
	}

	// 确保根目录存在
	if err := os.MkdirAll(config.LocalRootPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create local root directory: %w", err)
	}

	baseUploader := NewBaseUploader(StorageTypeLocal, config.SnowNode, config.FilePrefix)

	return &LocalUploader{
		BaseUploader: baseUploader,
		rootPath:     config.LocalRootPath,
		domain:       config.TencentCOSDomain, // 复用domain字段作为本地访问域名
	}, nil
}

// Upload 上传文件到本地
func (l *LocalUploader) Upload(ctx context.Context, fileName string, reader io.Reader, size int64) (*UploadResult, error) {
	_, span := l.StartSpan(ctx, "upload")
	defer span.End()

	span.SetAttributes(
		attribute.String("file.name", fileName),
		attribute.Int64("file.size", size),
		requestIDAttr(ctx),
	)

	// 生成文件路径
	filePath := l.GenerateFilePath(fileName)
	fullPath := filepath.Join(l.rootPath, filePath)

	// 确保目录存在
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		RecordError(span, err)
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// 创建文件
	file, err := os.Create(fullPath)
	if err != nil {
		RecordError(span, err)
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// 复制内容
	written, err := io.Copy(file, reader)
	if err != nil {
		RecordError(span, err)
		// 删除失败的文件
		removErr := os.Remove(fullPath) // 删除失败不影响主流程
		if removErr != nil {
			logger.WarnWithCtx(ctx, "failed to delete file",
				logger.String("file", fullPath),
				logger.Err(removErr),
			)
		}
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// 构建访问URL
	url := l.buildFileURL(filePath)

	span.SetAttributes(
		attribute.String("file.path", fullPath),
		attribute.String("file.url", url),
		attribute.Int64("file.written", written),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "upload success")

	return &UploadResult{
		URL:      url,
		Path:     filePath,
		Size:     written,
		MimeType: "",
		ExtraData: map[string]string{
			"full_path": fullPath,
		},
	}, nil
}

// GetPresignedURL 本地存储不支持预签名URL
func (l *LocalUploader) GetPresignedURL(_ context.Context, _ string, _ int64) (*PresignedURL, error) {
	_, span := l.StartSpan(context.Background(), "get_presigned_url")
	defer span.End()

	err := fmt.Errorf("local storage does not support presigned URL")
	RecordError(span, err)
	return nil, err
}

// Delete 删除本地文件
func (l *LocalUploader) Delete(ctx context.Context, path string) error {
	_, span := l.StartSpan(ctx, "delete")
	defer span.End()

	span.SetAttributes(attribute.String("file.path", path), requestIDAttr(ctx))

	fullPath := filepath.Join(l.rootPath, path)
	if err := os.Remove(fullPath); err != nil {
		RecordError(span, err)
		return fmt.Errorf("failed to delete file: %w", err)
	}

	span.SetStatus(codes.Ok, "delete success")
	return nil
}

// GetType 获取存储类型
func (l *LocalUploader) GetType() StorageType {
	return StorageTypeLocal
}

// buildFileURL 构建文件访问URL
func (l *LocalUploader) buildFileURL(filePath string) string {
	if l.domain != "" {
		return fmt.Sprintf("%s/%s", l.domain, filePath)
	}
	// 如果没有配置域名，返回相对路径
	return filePath
}
