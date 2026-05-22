package goupload

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"testing"

	"github.com/bwmarrin/snowflake"
	"github.com/stretchr/testify/require"
)

// TestExampleNewUploader 示例：创建上传器（推荐方式）
func TestExampleNewUploader(t *testing.T) {
	// 1. 本地存储
	localConfig := &UploaderConfig{
		Type:          StorageTypeLocal,
		FilePrefix:    "uploads",
		LocalRootPath: "/data/uploads",
	}
	localUploader, err := NewUploader(localConfig)
	require.NoError(t, err)

	// 2. 腾讯云COS
	cosConfig := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		FilePrefix:          "images",
		TencentCOSBucket:    "my-bucket",
		TencentCOSRegion:    "ap-beijing",
		TencentCOSSecretID:  "your-secret-id",
		TencentCOSSecretKey: "your-secret-key",
	}
	cosUploader, err := NewUploader(cosConfig)
	require.NoError(t, err)

	// 3. 阿里云OSS
	ossConfig := &UploaderConfig{
		Type:                     StorageTypeAliyunOSS,
		FilePrefix:               "videos",
		AliyunOSSEndpoint:        "oss-cn-hangzhou.aliyuncs.com",
		AliyunOSSBucket:          "my-bucket",
		AliyunOSSAccessKeyID:     "your-access-key-id",
		AliyunOSSAccessKeySecret: "your-access-key-secret",
	}
	ossUploader, err := NewUploader(ossConfig)
	require.NoError(t, err)

	// 使用统一的接口
	_ = localUploader
	_ = cosUploader
	_ = ossUploader
}

// TestExampleUploadFile 示例：上传文件
func TestExampleUploadFile(t *testing.T) {
	config := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		FilePrefix:          "images",
		TencentCOSBucket:    "my-bucket",
		TencentCOSRegion:    "ap-beijing",
		TencentCOSSecretID:  "your-secret-id",
		TencentCOSSecretKey: "your-secret-key",
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)

	ctx := context.Background()
	fileName := "photo.jpg"
	fileContent := []byte("image data...")
	reader := bytes.NewReader(fileContent)

	result, err := uploader.Upload(ctx, fileName, reader, int64(len(fileContent)))
	require.NoError(t, err)

	fmt.Printf("Upload success!\n")
	fmt.Printf("URL: %s\n", result.URL)
	fmt.Printf("Path: %s\n", result.Path)
	fmt.Printf("Size: %d\n", result.Size)
}

// TestExampleGetPresignedURL 示例：获取预签名URL（前端直传）
func TestExampleGetPresignedURL(t *testing.T) {
	config := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		FilePrefix:          "images",
		TencentCOSBucket:    "my-bucket",
		TencentCOSRegion:    "ap-beijing",
		TencentCOSSecretID:  "your-secret-id",
		TencentCOSSecretKey: "your-secret-key",
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)

	ctx := context.Background()
	fileName := "photo.jpg"
	expireSeconds := int64(300) // 5分钟

	presignedURL, err := uploader.GetPresignedURL(ctx, fileName, expireSeconds)
	require.NoError(t, err)

	fmt.Printf("Presigned URL: %s\n", presignedURL.URL)
	fmt.Printf("Path: %s\n", presignedURL.Path)
	fmt.Printf("Expire At: %s\n", presignedURL.ExpireAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Fields: %v\n", presignedURL.Fields)
}

// TestExampleDeleteFile 示例：删除文件
func TestExampleDeleteFile(t *testing.T) {
	config := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		FilePrefix:          "images",
		TencentCOSBucket:    "my-bucket",
		TencentCOSRegion:    "ap-beijing",
		TencentCOSSecretID:  "your-secret-id",
		TencentCOSSecretKey: "your-secret-key",
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)

	ctx := context.Background()
	filePath := "images/20260522/1234567890.jpg"

	err = uploader.Delete(ctx, filePath)
	require.NoError(t, err)

	fmt.Println("File deleted successfully")
}

// TestExampleWithContextTimeout 示例：使用超时控制
func TestExampleWithContextTimeout(t *testing.T) {
	config := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		FilePrefix:          "images",
		TencentCOSBucket:    "my-bucket",
		TencentCOSRegion:    "ap-beijing",
		TencentCOSSecretID:  "your-secret-id",
		TencentCOSSecretKey: "your-secret-key",
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)

	// 创建带超时的context
	ctx, cancel := context.WithTimeout(context.Background(), 30)
	defer cancel()

	fileName := "photo.jpg"
	fileContent := []byte("image data...")
	reader := bytes.NewReader(fileContent)

	result, err := uploader.Upload(ctx, fileName, reader, int64(len(fileContent)))
	require.NoError(t, err)

	fmt.Printf("Upload success: %s\n", result.URL)
}

// TestExampleMigrationFromOldCode 示例：从旧代码迁移
func TestExampleMigrationFromOldCode(t *testing.T) {
	// ===== 旧代码 =====
	/*
		node, _ := snowflake.NewNode(1)
		cosUploader := NewCosUploader(&CosUploaderInfo{
			Bucket:    "my-bucket",
			Region:    "ap-beijing",
			SecretID:  "xxx",
			SecretKey: "xxx",
			SnowNode:  node,
		})

		token, err := cosUploader.UploadCosPrepareData("photo.jpg")
	*/

	// ===== 新代码 =====
	config := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		FilePrefix:          "images", // 对应旧的 fileDirPrefix
		TencentCOSBucket:    "my-bucket",
		TencentCOSRegion:    "ap-beijing",
		TencentCOSSecretID:  "xxx",
		TencentCOSSecretKey: "xxx",
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)

	ctx := context.Background()
	presignedURL, err := uploader.GetPresignedURL(ctx, "photo.jpg", 300)
	require.NoError(t, err)

	// 旧代码的 token 字段映射到新代码
	/*
		old: token.PrefixURL      -> new: presignedURL.URL (完整URL)
		old: token.SavePath       -> new: presignedURL.Path
		old: token.QSignAlgorithm -> new: presignedURL.Fields["q-sign-algorithm"]
		old: token.QAk            -> new: presignedURL.Fields["q-ak"]
		old: token.QKeyTime       -> new: presignedURL.Fields["q-key-time"]
		old: token.QSignature     -> new: presignedURL.Fields["q-signature"]
		old: token.Policy         -> new: presignedURL.Fields["policy"]
	*/

	fmt.Printf("Migration complete!\n")
	fmt.Printf("URL: %s\n", presignedURL.URL)
	fmt.Printf("Path: %s\n", presignedURL.Path)
}

// TestExampleSwitchStorageBackend 示例：切换存储后端
func TestExampleSwitchStorageBackend(t *testing.T) {
	// 只需修改配置，无需改动业务代码

	// 开发环境：使用本地存储
	devConfig := &UploaderConfig{
		Type:          StorageTypeLocal,
		FilePrefix:    "uploads",
		LocalRootPath: "./uploads",
	}

	// 生产环境：使用腾讯云COS
	prodConfig := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		FilePrefix:          "uploads",
		TencentCOSBucket:    "prod-bucket",
		TencentCOSRegion:    "ap-shanghai",
		TencentCOSSecretID:  "prod-secret-id",
		TencentCOSSecretKey: "prod-secret-key",
	}

	// 根据环境选择配置
	isProd := true
	var config *UploaderConfig
	if isProd {
		config = prodConfig
	} else {
		config = devConfig
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)

	// 业务代码完全相同
	ctx := context.Background()
	fileName := "photo.jpg"
	fileContent := []byte("image data...")
	reader := bytes.NewReader(fileContent)

	result, err := uploader.Upload(ctx, fileName, reader, int64(len(fileContent)))
	require.NoError(t, err)

	fmt.Printf("Upload to %s: %s\n", uploader.GetType(), result.URL)
}

// TestExampleInGinHandler 示例：在 Gin Handler 中使用
func TestExampleInGinHandler(t *testing.T) {
	// 这个示例展示如何在 Gin 框架中使用
	// 实际使用时需要导入 gin 包

	/*
		import "github.com/gin-gonic/gin"

		func UploadHandler(c *gin.Context) {
			// 获取上传的文件
			file, header, err := c.Request.FormFile("file")
			if err != nil {
				c.JSON(400, gin.H{"error": "failed to get file"})
				return
			}
			defer file.Close()

			// 获取全局uploader实例
			uploader := global.GetUploader()

			// 使用请求的context（包含链路追踪信息）
			ctx := c.Request.Context()

			// 上传文件
			result, err := uploader.Upload(ctx, header.Filename, file, header.Size)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}

			// 返回结果
			c.JSON(200, gin.H{
				"code": 0,
				"data": gin.H{
					"url":  result.URL,
					"path": result.Path,
					"size": result.Size,
				},
			})
		}
	*/

	fmt.Println("See comments for Gin handler example")
}

// TestExampleCustomSnowNode 示例：自定义雪花节点
func TestExampleCustomSnowNode(t *testing.T) {
	// 创建自定义的雪花节点（节点ID为2）
	node, err := snowflake.NewNode(2)
	require.NoError(t, err)

	config := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		SnowNode:            node, // 传入自定义节点
		FilePrefix:          "images",
		TencentCOSBucket:    "my-bucket",
		TencentCOSRegion:    "ap-beijing",
		TencentCOSSecretID:  "xxx",
		TencentCOSSecretKey: "xxx",
	}

	uploader, err := NewUploader(config)
	require.NoError(t, err)

	_ = uploader
	fmt.Println("Custom snow node configured")
}

// TestExampleErrorHandling 示例：错误处理
func TestExampleErrorHandling(t *testing.T) {
	config := &UploaderConfig{
		Type:                StorageTypeTencentCOS,
		FilePrefix:          "images",
		TencentCOSBucket:    "my-bucket",
		TencentCOSRegion:    "ap-beijing",
		TencentCOSSecretID:  "xxx",
		TencentCOSSecretKey: "xxx",
	}

	uploader, err := NewUploader(config)
	if err != nil {
		// 初始化错误
		log.Printf("Failed to create uploader: %v", err)
		return
	}

	ctx := context.Background()
	fileName := "photo.jpg"
	fileContent := []byte("image data...")
	reader := bytes.NewReader(fileContent)

	result, err := uploader.Upload(ctx, fileName, reader, int64(len(fileContent)))
	if err != nil {
		// 上传错误
		log.Printf("Failed to upload file %s: %v", fileName, err)

		// 可以根据错误类型进行不同处理
		// 例如：网络错误可以重试，权限错误直接返回
		return
	}

	// 成功
	fmt.Printf("Upload success: %s\n", result.URL)
}
