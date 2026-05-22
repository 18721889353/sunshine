package goupload

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// AliyunOSSUploader 阿里云OSS上传器（使用官方SDK）
type AliyunOSSUploader struct {
	*BaseUploader
	client *oss.Client
	bucket *oss.Bucket
	domain string // 自定义域名
}

// NewAliyunOSSUploader 创建阿里云OSS上传器
func NewAliyunOSSUploader(config *UploaderConfig) (*AliyunOSSUploader, error) {
	if config.AliyunOSSEndpoint == "" || config.AliyunOSSBucket == "" {
		return nil, fmt.Errorf("endpoint and bucket cannot be empty")
	}
	if config.AliyunOSSAccessKeyID == "" || config.AliyunOSSAccessKeySecret == "" {
		return nil, fmt.Errorf("access key ID and secret cannot be empty")
	}

	// 创建OSS客户端
	client, err := oss.New(config.AliyunOSSEndpoint, config.AliyunOSSAccessKeyID, config.AliyunOSSAccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create OSS client: %w", err)
	}

	// 获取存储空间
	bucket, err := client.Bucket(config.AliyunOSSBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket: %w", err)
	}

	baseUploader := NewBaseUploader(StorageTypeAliyunOSS, config.SnowNode, config.FilePrefix)

	uploader := &AliyunOSSUploader{
		BaseUploader: baseUploader,
		client:       client,
		bucket:       bucket,
		domain:       config.AliyunOSSDomain,
	}

	return uploader, nil
}

// Upload 上传文件到OSS
func (a *AliyunOSSUploader) Upload(ctx context.Context, fileName string, reader io.Reader, size int64) (*UploadResult, error) {
	_, span := a.StartSpan(ctx, "upload")
	defer span.End()

	span.SetAttributes(
		attribute.String("file.name", fileName),
		attribute.Int64("file.size", size),
		attribute.String("bucket", a.bucket.BucketName),
	)

	// 生成文件路径
	objectKey := a.GenerateFilePath(fileName)
	span.SetAttributes(attribute.String("object.key", objectKey))

	// 使用OSS SDK上传
	err := a.bucket.PutObject(objectKey, reader)
	if err != nil {
		RecordError(span, err)
		return nil, fmt.Errorf("failed to upload to OSS: %w", err)
	}

	// 构建访问URL
	fileURL := a.buildFileURL(objectKey)

	span.SetAttributes(
		attribute.String("file.url", fileURL),
	)
	span.SetStatus(codes.Ok, "upload success")

	return &UploadResult{
		URL:      fileURL,
		Path:     objectKey,
		Size:     size,
		MimeType: "",
		ExtraData: map[string]string{
			"bucket":   a.bucket.BucketName,
			"endpoint": a.client.Config.Endpoint,
		},
	}, nil
}

// GetPresignedURL 获取预签名URL（用于前端直传）
func (a *AliyunOSSUploader) GetPresignedURL(ctx context.Context, fileName string, expireSeconds int64) (*PresignedURL, error) {
	_, span := a.StartSpan(ctx, "get_presigned_url")
	defer span.End()

	span.SetAttributes(
		attribute.String("file.name", fileName),
		attribute.Int64("expire_seconds", expireSeconds),
	)

	// 生成文件路径
	objectKey := a.GenerateFilePath(fileName)
	span.SetAttributes(attribute.String("object.key", objectKey))

	// 计算过期时间
	expireAt := time.Now().Add(time.Duration(expireSeconds) * time.Second)

	// 获取签名URL
	signedURL, err := a.bucket.SignURL(objectKey, oss.HTTPPut, expireSeconds)
	if err != nil {
		RecordError(span, err)
		return nil, fmt.Errorf("failed to get signed URL: %w", err)
	}

	span.SetAttributes(
		attribute.String("signed.url", signedURL),
		attribute.String("expire.at", expireAt.Format(time.RFC3339)),
	)
	span.SetStatus(codes.Ok, "signed URL generated")

	return &PresignedURL{
		URL:      signedURL,
		Path:     objectKey,
		ExpireAt: expireAt,
		Fields: map[string]string{
			"method": "PUT",
		},
		ExtraData: map[string]string{
			"bucket":   a.bucket.BucketName,
			"endpoint": a.client.Config.Endpoint,
		},
	}, nil
}

// Delete 删除OSS文件
func (a *AliyunOSSUploader) Delete(ctx context.Context, path string) error {
	_, span := a.StartSpan(ctx, "delete")
	defer span.End()

	span.SetAttributes(
		attribute.String("object.key", path),
		attribute.String("bucket", a.bucket.BucketName),
	)

	err := a.bucket.DeleteObject(path)
	if err != nil {
		RecordError(span, err)
		return fmt.Errorf("failed to delete from OSS: %w", err)
	}

	span.SetStatus(codes.Ok, "delete success")
	return nil
}

// GetType 获取存储类型
func (a *AliyunOSSUploader) GetType() StorageType {
	return StorageTypeAliyunOSS
}

// buildFileURL 构建文件访问URL
func (a *AliyunOSSUploader) buildFileURL(objectKey string) string {
	if a.domain != "" {
		return fmt.Sprintf("%s/%s", a.domain, objectKey)
	}
	// 使用默认OSS域名
	return fmt.Sprintf("https://%s.%s/%s", a.bucket.BucketName, a.client.Config.Endpoint, objectKey)
}
