// Package goupload 提供统一文件上传功能，支持本地、腾讯云COS、阿里云OSS等多种存储后端。
package goupload

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/bwmarrin/snowflake"
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
			return attribute.String("upload.request_id", reqID)
		}
	}
	return attribute.String("upload.request_id", "")
}

// StorageType 存储类型
type StorageType string

const (
	// StorageTypeLocal 本地存储
	StorageTypeLocal StorageType = "local"
	// StorageTypeTencentCOS 腾讯云COS
	StorageTypeTencentCOS StorageType = "tencent_cos"
	// StorageTypeAliyunOSS 阿里云OSS
	StorageTypeAliyunOSS StorageType = "aliyun_oss"
)

// UploadResult 上传结果
type UploadResult struct {
	URL       string            // 文件访问URL
	Path      string            // 文件存储路径
	Size      int64             // 文件大小
	MimeType  string            // 文件MIME类型
	ExtraData map[string]string // 额外数据（不同存储后端的特定信息）
}

// Uploader 统一上传器接口
type Uploader interface {
	// Upload 上传文件
	Upload(ctx context.Context, fileName string, reader io.Reader, size int64) (*UploadResult, error)

	// GetPresignedURL 获取预签名URL（用于前端直传）
	GetPresignedURL(ctx context.Context, fileName string, expireSeconds int64) (*PresignedURL, error)

	// Delete 删除文件
	Delete(ctx context.Context, path string) error

	// GetType 获取存储类型
	GetType() StorageType
}

// PresignedURL 预签名URL信息
type PresignedURL struct {
	URL       string            // 上传URL
	Path      string            // 文件路径
	Fields    map[string]string // 上传需要的字段（如policy、signature等）
	ExpireAt  time.Time         // 过期时间
	ExtraData map[string]string // 额外数据
}

// BaseUploader 基础上传器，提供通用功能
type BaseUploader struct {
	storageType StorageType
	snowNode    *snowflake.Node
	filePrefix  string
	tracer      trace.Tracer
}

// NewBaseUploader 创建基础上传器
func NewBaseUploader(storageType StorageType, snowNode *snowflake.Node, filePrefix string) *BaseUploader {
	if snowNode == nil {
		var err error
		snowNode, err = snowflake.NewNode(1)
		if err != nil {
			panic(fmt.Sprintf("failed to create snowflake node: %v", err))
		}
	}

	return &BaseUploader{
		storageType: storageType,
		snowNode:    snowNode,
		filePrefix:  filePrefix,
		tracer:      otel.Tracer(fmt.Sprintf("upload/%s", storageType)),
	}
}

// GenerateFilePath 生成文件存储路径
func (b *BaseUploader) GenerateFilePath(fileName string) string {
	ext := filepath.Ext(fileName)
	dateStr := time.Now().Format("20060102")
	uniqueName := b.snowNode.Generate().String()

	dir := fmt.Sprintf("%s/%s", b.filePrefix, dateStr)
	return fmt.Sprintf("%s/%s%s", dir, uniqueName, ext)
}

// StartSpan 创建追踪Span
func (b *BaseUploader) StartSpan(ctx context.Context, operation string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, span := b.tracer.Start(ctx, fmt.Sprintf("%s.%s", b.storageType, operation), opts...)
	span.SetAttributes(
		attribute.String("storage.type", string(b.storageType)),
		requestIDAttr(ctx),
	)
	return ctx, span
}

// RecordError 记录错误到Span
func RecordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// UploaderConfig 上传器配置
type UploaderConfig struct {
	Type       StorageType     // 存储类型
	SnowNode   *snowflake.Node // 雪花算法节点
	FilePrefix string          // 文件前缀目录

	// 本地存储配置
	LocalRootPath string

	// 腾讯云COS配置
	TencentCOSBucket    string
	TencentCOSRegion    string
	TencentCOSSecretID  string
	TencentCOSSecretKey string
	TencentCOSDomain    string // 自定义域名

	// 阿里云OSS配置
	AliyunOSSEndpoint        string
	AliyunOSSBucket          string
	AliyunOSSAccessKeyID     string
	AliyunOSSAccessKeySecret string
	AliyunOSSDomain          string // 自定义域名
}

// NewUploader 工厂方法：根据配置创建上传器
func NewUploader(config *UploaderConfig) (Uploader, error) {
	if config.SnowNode == nil {
		var err error
		config.SnowNode, err = snowflake.NewNode(1)
		if err != nil {
			return nil, fmt.Errorf("failed to create snowflake node: %w", err)
		}
	}

	switch config.Type {
	case StorageTypeLocal:
		return NewLocalUploader(config)
	case StorageTypeTencentCOS:
		return NewTencentCOSUploader(config)
	case StorageTypeAliyunOSS:
		return NewAliyunOSSUploader(config)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}
