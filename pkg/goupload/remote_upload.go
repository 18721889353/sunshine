package goupload

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DownloadAndUpload 从远程URL下载图片并上传到存储
// 支持HTTP和HTTPS协议的图片URL
func (u *UploaderHelper) DownloadAndUpload(ctx context.Context, imageURL string, customFileName ...string) (*UploadResult, error) {
	// 从URL中提取文件名
	fileName := extractFileNameFromURL(imageURL)
	if len(customFileName) > 0 && customFileName[0] != "" {
		fileName = customFileName[0]
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加常见的请求头，避免被反爬虫拦截
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	// 发送请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	//nolint:errcheck // 在defer中忽略Close错误是常见做法
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// 获取Content-Type
	contentType := resp.Header.Get("Content-Type")

	// 如果没有提供文件名，尝试从Content-Type推断扩展名
	if fileName == "" || !strings.Contains(fileName, ".") {
		if ext := getExtensionFromContentType(contentType); ext != "" {
			if fileName == "" {
				fileName = "image" + ext
			} else {
				fileName = fileName + ext
			}
		}
	}

	// 获取文件大小（如果服务器提供了Content-Length）
	size := resp.ContentLength

	// 上传到存储
	return u.uploader.Upload(ctx, fileName, resp.Body, size)
}

// DownloadAndUploadWithSize 从远程URL下载图片并上传，指定最大文件大小
func (u *UploaderHelper) DownloadAndUploadWithSize(ctx context.Context, imageURL string, maxSize int64, customFileName ...string) (*UploadResult, error) {
	// 从URL中提取文件名
	fileName := extractFileNameFromURL(imageURL)
	if len(customFileName) > 0 && customFileName[0] != "" {
		fileName = customFileName[0]
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加常见的请求头
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	// 发送请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	//nolint:errcheck // 在defer中忽略Close错误是常见做法
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// 检查文件大小
	if resp.ContentLength > maxSize {
		return nil, fmt.Errorf("file size %d exceeds maximum allowed size %d", resp.ContentLength, maxSize)
	}

	// 获取Content-Type
	contentType := resp.Header.Get("Content-Type")

	// 如果没有提供文件名，尝试从Content-Type推断扩展名
	if fileName == "" || !strings.Contains(fileName, ".") {
		if ext := getExtensionFromContentType(contentType); ext != "" {
			if fileName == "" {
				fileName = "image" + ext
			} else {
				fileName = fileName + ext
			}
		}
	}

	// 使用LimitReader限制读取大小
	limitedReader := io.LimitReader(resp.Body, maxSize)

	// 上传到存储
	return u.uploader.Upload(ctx, fileName, limitedReader, resp.ContentLength)
}

// UploaderHelper 上传辅助工具
type UploaderHelper struct {
	uploader Uploader
}

// NewUploaderHelper 创建上传辅助工具
func NewUploaderHelper(uploader Uploader) *UploaderHelper {
	return &UploaderHelper{
		uploader: uploader,
	}
}

// extractFileNameFromURL 从URL中提取文件名
func extractFileNameFromURL(imageURL string) string {
	// 解析URL获取路径部分
	parts := strings.Split(imageURL, "/")
	if len(parts) == 0 {
		return ""
	}

	// 获取最后一部分作为文件名
	fileName := parts[len(parts)-1]

	// 移除URL参数（?后面的部分）
	if idx := strings.Index(fileName, "?"); idx != -1 {
		fileName = fileName[:idx]
	}

	// 验证是否是有效的文件名
	if fileName == "" || strings.Contains(fileName, "/") {
		return ""
	}

	return fileName
}

// getExtensionFromContentType 从Content-Type获取文件扩展名
func getExtensionFromContentType(contentType string) string {
	// 移除参数部分（;后面的部分）
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = contentType[:idx]
	}

	contentType = strings.TrimSpace(contentType)

	switch contentType {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	case "image/tiff":
		return ".tiff"
	case "image/x-icon":
		return ".ico"
	default:
		// 尝试从Content-Type中提取扩展名
		if strings.HasPrefix(contentType, "image/") {
			return "." + strings.TrimPrefix(contentType, "image/")
		}
		return ""
	}
}

// DownloadAndUpload 便捷函数：从远程URL下载并上传
func DownloadAndUpload(ctx context.Context, uploader Uploader, imageURL string, customFileName ...string) (*UploadResult, error) {
	helper := NewUploaderHelper(uploader)
	return helper.DownloadAndUpload(ctx, imageURL, customFileName...)
}

// ValidateImageURL 验证图片URL是否有效
func ValidateImageURL(ctx context.Context, imageURL string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, imageURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to validate URL: %w", err)
	}
	//nolint:errcheck // 在defer中忽略Close错误是常见做法
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("URL validation failed with status: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return "", 0, fmt.Errorf("URL does not point to an image (Content-Type: %s)", contentType)
	}

	size := resp.ContentLength
	return contentType, size, nil
}

// UploadFromReader 从 io.Reader上传（保留原有功能）
func UploadFromReader(ctx context.Context, uploader Uploader, fileName string, reader io.Reader, size int64) (*UploadResult, error) {
	result, err := uploader.Upload(ctx, fileName, reader, size)
	if err != nil {
		return nil, err
	}

	return result, nil
}
