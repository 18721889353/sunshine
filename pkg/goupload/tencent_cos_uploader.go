package goupload

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// TencentCOSUploader 腾讯云COS上传器（使用官方SDK）
type TencentCOSUploader struct {
	*BaseUploader
	client *cos.Client
	bucket string
	region string
	domain string // 自定义域名
}

// NewTencentCOSUploader 创建腾讯云COS上传器
func NewTencentCOSUploader(config *UploaderConfig) (*TencentCOSUploader, error) {
	if config.TencentCOSBucket == "" || config.TencentCOSRegion == "" {
		return nil, fmt.Errorf("bucket and region cannot be empty")
	}
	if config.TencentCOSSecretID == "" || config.TencentCOSSecretKey == "" {
		return nil, fmt.Errorf("secret ID and secret key cannot be empty")
	}

	// 构建COS客户端
	baseURLStr := fmt.Sprintf("https://%s.cos.%s.myqcloud.com", config.TencentCOSBucket, config.TencentCOSRegion)
	baseURL, err := url.Parse(baseURLStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse bucket URL: %w", err)
	}

	client := cos.NewClient(&cos.BaseURL{BucketURL: baseURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.TencentCOSSecretID,
			SecretKey: config.TencentCOSSecretKey,
		},
	})

	baseUploader := NewBaseUploader(StorageTypeTencentCOS, config.SnowNode, config.FilePrefix)

	uploader := &TencentCOSUploader{
		BaseUploader: baseUploader,
		client:       client,
		bucket:       config.TencentCOSBucket,
		region:       config.TencentCOSRegion,
		domain:       config.TencentCOSDomain,
	}

	return uploader, nil
}

// Upload 上传文件到COS
func (t *TencentCOSUploader) Upload(ctx context.Context, fileName string, reader io.Reader, size int64) (*UploadResult, error) {
	_, span := t.StartSpan(ctx, "upload")
	defer span.End()

	span.SetAttributes(
		attribute.String("file.name", fileName),
		attribute.Int64("file.size", size),
		attribute.String("bucket", t.bucket),
		attribute.String("region", t.region),
	)

	// 生成文件路径
	objectKey := t.GenerateFilePath(fileName)
	span.SetAttributes(attribute.String("object.key", objectKey))

	// 使用COS SDK上传
	opt := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentLength: size,
		},
	}

	_, err := t.client.Object.Put(ctx, objectKey, reader, opt)
	if err != nil {
		RecordError(span, err)
		return nil, fmt.Errorf("failed to upload to COS: %w", err)
	}

	// 构建访问URL
	fileURL := t.buildFileURL(objectKey)

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
			"bucket": t.bucket,
			"region": t.region,
		},
	}, nil
}

// GetPresignedURL 获取预签名URL（用于前端直传）
func (t *TencentCOSUploader) GetPresignedURL(ctx context.Context, fileName string, expireSeconds int64) (*PresignedURL, error) {
	_, span := t.StartSpan(ctx, "get_presigned_url")
	defer span.End()

	span.SetAttributes(
		attribute.String("file.name", fileName),
		attribute.Int64("expire_seconds", expireSeconds),
	)

	// 生成文件路径
	objectKey := t.GenerateFilePath(fileName)
	span.SetAttributes(attribute.String("object.key", objectKey))

	// 计算过期时间
	expireAt := time.Now().Add(time.Duration(expireSeconds) * time.Second)

	// 获取预签名URL
	presignedURL, err := t.client.Object.GetPresignedURL(
		ctx,
		http.MethodPut,
		objectKey,
		t.client.GetCredential().SecretID,
		t.client.GetCredential().SecretKey,
		time.Duration(expireSeconds)*time.Second,
		nil,
	)
	if err != nil {
		RecordError(span, err)
		return nil, fmt.Errorf("failed to get presigned URL: %w", err)
	}

	span.SetAttributes(
		attribute.String("presigned.url", presignedURL.String()),
		attribute.String("expire.at", expireAt.Format(time.RFC3339)),
	)
	span.SetStatus(codes.Ok, "presigned URL generated")

	return &PresignedURL{
		URL:      presignedURL.String(),
		Path:     objectKey,
		ExpireAt: expireAt,
		Fields: map[string]string{
			"method": http.MethodPut,
		},
		ExtraData: map[string]string{
			"bucket": t.bucket,
			"region": t.region,
		},
	}, nil
}

// Delete 删除COS文件
func (t *TencentCOSUploader) Delete(ctx context.Context, path string) error {
	_, span := t.StartSpan(ctx, "delete")
	defer span.End()

	span.SetAttributes(
		attribute.String("object.key", path),
		attribute.String("bucket", t.bucket),
	)

	_, err := t.client.Object.Delete(ctx, path)
	if err != nil {
		RecordError(span, err)
		return fmt.Errorf("failed to delete from COS: %w", err)
	}

	span.SetStatus(codes.Ok, "delete success")
	return nil
}

// GetType 获取存储类型
func (t *TencentCOSUploader) GetType() StorageType {
	return StorageTypeTencentCOS
}

// buildFileURL 构建文件访问URL
func (t *TencentCOSUploader) buildFileURL(objectKey string) string {
	if t.domain != "" {
		return fmt.Sprintf("%s/%s", t.domain, objectKey)
	}
	// 使用默认COS域名
	return fmt.Sprintf("https://%s.cos.%s.myqcloud.com/%s", t.bucket, t.region, objectKey)
}
