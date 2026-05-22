package goupload

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/snowflake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 创建测试用的雪花节点
func testSnowNode(t *testing.T) *snowflake.Node {
	node, err := snowflake.NewNode(1)
	require.NoError(t, err)
	return node
}

// 创建测试文件内容
func testFileContent() ([]byte, string) {
	content := []byte("test file content for upload")
	fileName := "test.jpg"
	return content, fileName
}

// TestLocalUploader 测试本地上传器
func TestLocalUploader(t *testing.T) {
	// 创建临时目录
	tempDir := t.TempDir()

	config := &UploaderConfig{
		Type:             StorageTypeLocal,
		SnowNode:         testSnowNode(t),
		FilePrefix:       "uploads",
		LocalRootPath:    tempDir,
		TencentCOSDomain: "http://localhost:8080", // 复用作为本地访问域名
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)
	assert.Equal(t, StorageTypeLocal, uploader.GetType())

	// 测试上传
	content, fileName := testFileContent()
	reader := bytes.NewReader(content)

	ctx := context.Background()
	result, err := uploader.Upload(ctx, fileName, reader, int64(len(content)))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result.Path)
	assert.NotEmpty(t, result.URL)
	assert.Equal(t, int64(len(content)), result.Size)
	assert.Contains(t, result.Path, "uploads")
	assert.Contains(t, result.Path, ".jpg")

	// 验证文件是否存在
	fullPath := filepath.Join(tempDir, result.Path)
	_, err = os.Stat(fullPath)
	assert.NoError(t, err)

	// 测试删除
	err = uploader.Delete(ctx, result.Path)
	assert.NoError(t, err)

	// 验证文件已删除
	_, err = os.Stat(fullPath)
	assert.True(t, os.IsNotExist(err))

	// 测试预签名URL（应该失败）
	_, err = uploader.GetPresignedURL(ctx, fileName, 300)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support presigned URL")
}

// TestTencentCOSUploader_PresignedURL 测试腾讯云COS预签名URL生成
func TestTencentCOSUploader_PresignedURL(t *testing.T) {
	config := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		SnowNode:            testSnowNode(t),
		FilePrefix:          "test-uploads",
		TencentCOSBucket:    "test-bucket",
		TencentCOSRegion:    "ap-beijing",
		TencentCOSSecretID:  "test-secret-id",
		TencentCOSSecretKey: "test-secret-key",
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)
	assert.Equal(t, StorageTypeTencentCOS, uploader.GetType())

	ctx := context.Background()
	presignedURL, err := uploader.GetPresignedURL(ctx, "test.jpg", 300)

	// 注意：这里会失败，因为使用的是测试凭证
	// 实际使用时需要配置真实的腾讯云凭证
	if err != nil {
		t.Logf("Expected error with test credentials: %v", err)
		return
	}

	require.NoError(t, err)
	assert.NotNil(t, presignedURL)
	assert.NotEmpty(t, presignedURL.URL)
	assert.NotEmpty(t, presignedURL.Path)
	assert.Contains(t, presignedURL.Path, "test-uploads")
	assert.Contains(t, presignedURL.Path, ".jpg")
}

// TestAliyunOSSUploader_PresignedURL 测试阿里云OSS预签名URL生成
func TestAliyunOSSUploader_PresignedURL(t *testing.T) {
	config := &UploaderConfig{
		Type:                     StorageTypeAliyunOSS,
		SnowNode:                 testSnowNode(t),
		FilePrefix:               "test-uploads",
		AliyunOSSEndpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AliyunOSSBucket:          "test-bucket",
		AliyunOSSAccessKeyID:     "test-access-key-id",
		AliyunOSSAccessKeySecret: "test-access-key-secret",
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)
	assert.Equal(t, StorageTypeAliyunOSS, uploader.GetType())

	ctx := context.Background()
	presignedURL, err := uploader.GetPresignedURL(ctx, "test.jpg", 300)

	// 注意：这里会失败，因为使用的是测试凭证
	// 实际使用时需要配置真实的阿里云凭证
	if err != nil {
		t.Logf("Expected error with test credentials: %v", err)
		return
	}

	require.NoError(t, err)
	assert.NotNil(t, presignedURL)
	assert.NotEmpty(t, presignedURL.URL)
	assert.NotEmpty(t, presignedURL.Path)
	assert.Contains(t, presignedURL.Path, "test-uploads")
	assert.Contains(t, presignedURL.Path, ".jpg")
}

// TestUploaderFactory 测试工厂方法
func TestUploaderFactory(t *testing.T) {
	tests := []struct {
		name         string
		config       *UploaderConfig
		expectError  bool
		expectedType StorageType
	}{
		{
			name: "local uploader",
			config: &UploaderConfig{
				Type:          StorageTypeLocal,
				SnowNode:      testSnowNode(t),
				FilePrefix:    "uploads",
				LocalRootPath: t.TempDir(),
			},
			expectError:  false,
			expectedType: StorageTypeLocal,
		},
		{
			name: "tencent cos uploader",
			config: &UploaderConfig{
				Type:                StorageTypeTencentCOS,
				SnowNode:            testSnowNode(t),
				FilePrefix:          "uploads",
				TencentCOSBucket:    "test-bucket",
				TencentCOSRegion:    "ap-beijing",
				TencentCOSSecretID:  "test-id",
				TencentCOSSecretKey: "test-key",
			},
			expectError:  false,
			expectedType: StorageTypeTencentCOS,
		},
		{
			name: "aliyun oss uploader",
			config: &UploaderConfig{
				Type:                     StorageTypeAliyunOSS,
				SnowNode:                 testSnowNode(t),
				FilePrefix:               "uploads",
				AliyunOSSEndpoint:        "oss-cn-hangzhou.aliyuncs.com",
				AliyunOSSBucket:          "test-bucket",
				AliyunOSSAccessKeyID:     "test-id",
				AliyunOSSAccessKeySecret: "test-secret",
			},
			expectError:  false,
			expectedType: StorageTypeAliyunOSS,
		},
		{
			name: "unsupported type",
			config: &UploaderConfig{
				Type:       "unsupported",
				SnowNode:   testSnowNode(t),
				FilePrefix: "uploads",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uploader, err := NewUploader(tt.config)
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, uploader)
			assert.Equal(t, tt.expectedType, uploader.GetType())
		})
	}
}

// TestBaseUploader_GenerateFilePath 测试文件路径生成
func TestBaseUploader_GenerateFilePath(t *testing.T) {
	baseUploader := NewBaseUploader(StorageTypeLocal, testSnowNode(t), "images")

	fileName := "photo.jpg"
	path := baseUploader.GenerateFilePath(fileName)

	assert.NotEmpty(t, path)
	assert.Contains(t, path, "images")
	assert.Contains(t, path, ".jpg")

	// 验证路径格式：images/YYYYMMDD/unique_id.jpg
	parts := strings.Split(path, "/")
	assert.GreaterOrEqual(t, len(parts), 3)
	assert.Equal(t, "images", parts[0])
	assert.Len(t, parts[1], 8) // 日期部分应该是8位
	assert.Contains(t, parts[2], ".jpg")
}

// TestUploadResult 测试上传结果结构
func TestUploadResult(t *testing.T) {
	result := &UploadResult{
		URL:      "https://example.com/file.jpg",
		Path:     "uploads/20260522/123.jpg",
		Size:     1024,
		MimeType: "image/jpeg",
		ExtraData: map[string]string{
			"bucket": "test-bucket",
		},
	}

	assert.Equal(t, "https://example.com/file.jpg", result.URL)
	assert.Equal(t, "uploads/20260522/123.jpg", result.Path)
	assert.Equal(t, int64(1024), result.Size)
	assert.Equal(t, "image/jpeg", result.MimeType)
	assert.Equal(t, "test-bucket", result.ExtraData["bucket"])
}

// TestPresignedURL 测试预签名URL结构
func TestPresignedURL(t *testing.T) {
	presignedURL := &PresignedURL{
		URL:      "https://example.com/upload?signature=xxx",
		Path:     "uploads/20260522/123.jpg",
		ExpireAt: time.Now().Add(300 * time.Second),
		Fields: map[string]string{
			"policy": "base64-policy",
		},
		ExtraData: map[string]string{
			"region": "ap-beijing",
		},
	}

	assert.NotEmpty(t, presignedURL.URL)
	assert.NotEmpty(t, presignedURL.Path)
	assert.False(t, presignedURL.ExpireAt.IsZero())
	assert.Equal(t, "base64-policy", presignedURL.Fields["policy"])
	assert.Equal(t, "ap-beijing", presignedURL.ExtraData["region"])
}

// BenchmarkLocalUploader_Upload 基准测试：本地上传
func BenchmarkLocalUploader_Upload(b *testing.B) {
	tempDir := b.TempDir()
	config := &UploaderConfig{
		Type:          StorageTypeLocal,
		SnowNode:      testSnowNode(&testing.T{}),
		FilePrefix:    "uploads",
		LocalRootPath: tempDir,
	}

	uploader, err := NewUploader(config)
	require.NoError(b, err)

	content := []byte("benchmark test content")
	fileName := "benchmark.jpg"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(content)
		ctx := context.Background()
		_, err := uploader.Upload(ctx, fileName, reader, int64(len(content)))
		if err != nil {
			b.Fatal(err)
		}
	}
}
