# 文件上传模块 - 完整使用指南

## 📋 概述

`goupload` 是一个企业级统一文件上传模块，支持多种存储后端：

- 📁 **本地存储** (Local) - 适合开发环境
- ☁️ **腾讯云COS** (Tencent COS) - 使用官方SDK
- ☁️ **阿里云OSS** (Aliyun OSS) - 使用官方SDK

### ✨ 核心特性

✅ **统一接口** - 所有存储后端实现相同的 `Uploader` 接口  
✅ **链路追踪** - 集成 OpenTelemetry，自动记录完整调用链路  
✅ **Context支持** - 所有方法都支持 `context.Context`  
✅ **工厂模式** - 通过配置轻松切换存储后端  
✅ **预签名URL** - 支持前端直传（COS/OSS）  
✅ **雪花算法** - 自动生成唯一文件名  

---

## 🚀 快速开始

### 1. 安装依赖

```bash
go get github.com/tencentyun/cos-go-sdk-v5
go get github.com/aliyun/aliyun-oss-go-sdk/oss
```

### 2. 基础示例

```go
package main

import (
    "bytes"
    "context"
    "fmt"
    "log"
    
    "github.com/18721889353/sunshine/pkg/goupload"
)

func main() {
    // 配置存储后端
    config := &upload.UploaderConfig{
        Type:          upload.StorageTypeLocal,
        FilePrefix:    "uploads",
        LocalRootPath: "./uploads",
    }
    
    // 创建上传器
    uploader, err := upload.NewUploader(config)
    if err != nil {
        log.Fatal(err)
    }
    
    // 上传文件
    ctx := context.Background()
    fileName := "photo.jpg"
    fileContent := []byte("image data...")
    reader := bytes.NewReader(fileContent)
    
    result, err := uploader.Upload(ctx, fileName, reader, int64(len(fileContent)))
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("上传成功！\n")
    fmt.Printf("URL: %s\n", result.URL)
    fmt.Printf("路径: %s\n", result.Path)
}
```

---

## 📦 存储后端配置

### 1️⃣ 本地存储（开发环境推荐）

```go
config := &upload.UploaderConfig{
    Type:          upload.StorageTypeLocal,
    FilePrefix:    "uploads",           // 文件前缀目录
    LocalRootPath: "/data/uploads",     // 本地根目录
    TencentCOSDomain: "https://example.com/files", // 可选：访问域名
}

uploader, err := upload.NewUploader(config)
```

**特点：**
- ✅ 适合开发环境和小型应用
- ❌ 不支持预签名URL
- ⚠️ 需要确保磁盘空间充足

---

### 2️⃣ 腾讯云COS（生产环境）

```go
config := &upload.UploaderConfig{
    Type:                upload.StorageTypeTencentCOS,
    FilePrefix:          "images",
    TencentCOSBucket:    "my-bucket-123456",
    TencentCOSRegion:    "ap-beijing",
    TencentCOSSecretID:  "AKIDxxxxxxxxxxxxx",
    TencentCOSSecretKey: "xxxxxxxxxxxxx",
    TencentCOSDomain:    "https://cdn.example.com", // 可选：CDN域名
}

uploader, err := upload.NewUploader(config)
```

**特点：**
- ✅ 使用腾讯云官方SDK
- ✅ 支持预签名URL（前端直传）
- ✅ 高可用、高可靠
- ✅ 可配合CDN加速

---

### 3️⃣ 阿里云OSS（生产环境）

```go
config := &upload.UploaderConfig{
    Type:                     upload.StorageTypeAliyunOSS,
    FilePrefix:               "videos",
    AliyunOSSEndpoint:        "oss-cn-hangzhou.aliyuncs.com",
    AliyunOSSBucket:          "my-bucket",
    AliyunOSSAccessKeyID:     "LTAIxxxxxxxxxxxxx",
    AliyunOSSAccessKeySecret: "xxxxxxxxxxxxx",
    AliyunOSSDomain:          "https://cdn.example.com", // 可选：CDN域名
}

uploader, err := upload.NewUploader(config)
```

**特点：**
- ✅ 使用阿里云官方SDK
- ✅ 支持签名URL（前端直传）
- ✅ 全球部署，多区域支持
- ✅ 丰富的数据处理能力

---

## 💡 详细使用案例

### 案例1：在 Gin 框架中上传文件

#### 步骤1：初始化全局上传器

```go
// global/uploader.go
package global

import (
    "sync"
    "github.com/18721889353/sunshine/pkg/goupload"
)

var (
    uploader     upload.Uploader
    uploaderOnce sync.Once
)

func InitUploader() {
    uploaderOnce.Do(func() {
        config := &upload.UploaderConfig{
            Type:                upload.StorageTypeTencentCOS,
            FilePrefix:          "uploads",
            TencentCOSBucket:    "your-bucket",
            TencentCOSRegion:    "ap-beijing",
            TencentCOSSecretID:  "your-secret-id",
            TencentCOSSecretKey: "your-secret-key",
        }
        
        var err error
        uploader, err = upload.NewUploader(config)
        if err != nil {
            panic(err)
        }
    })
}

func GetUploader() upload.Uploader {
    return uploader
}
```

#### 步骤2：创建上传Handler

```go
// handler/upload.go
package handler

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "github.com/18721889353/sunshine/global"
)

func UploadFile(c *gin.Context) {
    // 获取上传的文件
    file, header, err := c.Request.FormFile("file")
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{
            "code": 400,
            "msg":  "获取文件失败",
        })
        return
    }
    defer file.Close()
    
    // 文件类型验证
    allowedExts := map[string]bool{
        ".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
    }
    ext := filepath.Ext(header.Filename)
    if !allowedExts[strings.ToLower(ext)] {
        c.JSON(http.StatusBadRequest, gin.H{
            "code": 400,
            "msg":  "不支持的文件类型",
        })
        return
    }
    
    // 文件大小验证（10MB）
    const maxFileSize = 10 * 1024 * 1024
    if header.Size > maxFileSize {
        c.JSON(http.StatusBadRequest, gin.H{
            "code": 400,
            "msg":  "文件大小不能超过10MB",
        })
        return
    }
    
    // 获取全局上传器
    uploader := global.GetUploader()
    
    // 使用请求的context（包含链路追踪）
    ctx := c.Request.Context()
    
    // 上传文件
    result, err := uploader.Upload(ctx, header.Filename, file, header.Size)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "code": 500,
            "msg":  "上传失败: " + err.Error(),
        })
        return
    }
    
    // 返回结果
    c.JSON(http.StatusOK, gin.H{
        "code": 0,
        "msg":  "上传成功",
        "data": gin.H{
            "url":  result.URL,
            "path": result.Path,
            "size": result.Size,
        },
    })
}
```

#### 步骤3：注册路由

```go
// router/router.go
package router

import (
    "github.com/gin-gonic/gin"
    "your_project/handler"
)

func SetupRouter() *gin.Engine {
    r := gin.Default()
    
    // 上传接口
    r.POST("/api/upload", handler.UploadFile)
    
    return r
}
```

---

### 案例2：前端直传（预签名URL）

#### 后端：生成预签名URL

```go
// handler/presigned.go
package handler

import (
    "net/http"
    "time"
    "github.com/gin-gonic/gin"
    "github.com/18721889353/sunshine/global"
)

func GetPresignedURL(c *gin.Context) {
    fileName := c.Query("file_name")
    if fileName == "" {
        c.JSON(http.StatusBadRequest, gin.H{
            "code": 400,
            "msg":  "file_name is required",
        })
        return
    }
    
    // 文件类型验证
    allowedExts := map[string]bool{
        ".jpg": true, ".jpeg": true, ".png": true,
    }
    ext := filepath.Ext(fileName)
    if !allowedExts[strings.ToLower(ext)] {
        c.JSON(http.StatusBadRequest, gin.H{
            "code": 400,
            "msg":  "不支持的文件类型",
        })
        return
    }
    
    uploader := global.GetUploader()
    ctx := c.Request.Context()
    
    // 生成5分钟有效的预签名URL
    expireSeconds := int64(300)
    presignedURL, err := uploader.GetPresignedURL(ctx, fileName, expireSeconds)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "code": 500,
            "msg":  "生成签名失败: " + err.Error(),
        })
        return
    }
    
    c.JSON(http.StatusOK, gin.H{
        "code": 0,
        "msg":  "success",
        "data": gin.H{
            "url":      presignedURL.URL,
            "path":     presignedURL.Path,
            "expire":   presignedURL.ExpireAt.Unix(),
            "expireAt": presignedURL.ExpireAt.Format(time.RFC3339),
            "fields":   presignedURL.Fields,
        },
    })
}
```

#### 前端：直接上传到COS/OSS

```javascript
// 1. 获取预签名URL
async function getPresignedUrl(fileName) {
    const response = await fetch(`/api/presigned-url?file_name=${fileName}`);
    const { code, data } = await response.json();
    
    if (code !== 0) {
        throw new Error('获取签名失败');
    }
    
    return data;
}

// 2. 直接上传文件到COS/OSS
async function uploadFile(file) {
    try {
        // 获取预签名URL
        const { url, path } = await getPresignedUrl(file.name);
        
        // 直接上传文件
        const uploadResponse = await fetch(url, {
            method: 'PUT',
            body: file,
            headers: {
                'Content-Type': file.type,
            },
        });
        
        if (!uploadResponse.ok) {
            throw new Error('上传失败');
        }
        
        console.log('上传成功，文件路径:', path);
        return path;
        
    } catch (error) {
        console.error('上传错误:', error);
        throw error;
    }
}

// 使用示例
document.getElementById('file-input').addEventListener('change', async (e) => {
    const file = e.target.files[0];
    if (file) {
        try {
            const path = await uploadFile(file);
            alert('上传成功！文件路径: ' + path);
        } catch (error) {
            alert('上传失败: ' + error.message);
        }
    }
});
```

---

### 案例3：从配置文件加载

```go
// configs/upload.go
package config

import (
    "os"
    "github.com/18721889353/sunshine/pkg/goupload"
)

func GetUploadConfig() *upload.UploaderConfig {
    // 从环境变量读取存储类型
    storageType := os.Getenv("STORAGE_TYPE") // "local", "tencent_cos", "aliyun_oss"
    
    config := &upload.UploaderConfig{
        FilePrefix: "uploads",
    }
    
    switch storageType {
    case "local":
        config.Type = upload.StorageTypeLocal
        config.LocalRootPath = os.Getenv("LOCAL_ROOT_PATH")
        
    case "tencent_cos":
        config.Type = upload.StorageTypeTencentCOS
        config.TencentCOSBucket = os.Getenv("COS_BUCKET")
        config.TencentCOSRegion = os.Getenv("COS_REGION")
        config.TencentCOSSecretID = os.Getenv("COS_SECRET_ID")
        config.TencentCOSSecretKey = os.Getenv("COS_SECRET_KEY")
        config.TencentCOSDomain = os.Getenv("COS_DOMAIN")
        
    case "aliyun_oss":
        config.Type = upload.StorageTypeAliyunOSS
        config.AliyunOSSEndpoint = os.Getenv("OSS_ENDPOINT")
        config.AliyunOSSBucket = os.Getenv("OSS_BUCKET")
        config.AliyunOSSAccessKeyID = os.Getenv("OSS_ACCESS_KEY_ID")
        config.AliyunOSSAccessKeySecret = os.Getenv("OSS_ACCESS_KEY_SECRET")
        config.AliyunOSSDomain = os.Getenv("OSS_DOMAIN")
        
    default:
        panic("unsupported storage type: " + storageType)
    }
    
    return config
}
```

**环境变量配置示例（.env文件）：**

```bash
# 开发环境
STORAGE_TYPE=local
LOCAL_ROOT_PATH=./uploads

# 生产环境 - 腾讯云COS
# STORAGE_TYPE=tencent_cos
# COS_BUCKET=my-bucket-123456
# COS_REGION=ap-beijing
# COS_SECRET_ID=AKIDxxxxxxxxxxxxx
# COS_SECRET_KEY=xxxxxxxxxxxxx
# COS_DOMAIN=https://cdn.example.com

# 生产环境 - 阿里云OSS
# STORAGE_TYPE=aliyun_oss
# OSS_ENDPOINT=oss-cn-hangzhou.aliyuncs.com
# OSS_BUCKET=my-bucket
# OSS_ACCESS_KEY_ID=LTAIxxxxxxxxxxxxx
# OSS_ACCESS_KEY_SECRET=xxxxxxxxxxxxx
# OSS_DOMAIN=https://cdn.example.com
```

---

### 案例4：带超时控制的上传

```go
package main

import (
    "context"
    "time"
    "log"
    
    "github.com/18721889353/sunshine/pkg/goupload"
)

func UploadWithTimeout(uploader upload.Uploader, fileName string, reader io.Reader, size int64) (*upload.UploadResult, error) {
    // 创建带30秒超时的context
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // 上传文件
    result, err := uploader.Upload(ctx, fileName, reader, size)
    if err != nil {
        // 检查是否是超时错误
        if ctx.Err() == context.DeadlineExceeded {
            log.Printf("上传超时: %s", fileName)
            return nil, fmt.Errorf("上传超时，请稍后重试")
        }
        return nil, err
    }
    
    return result, nil
}
```

---

### 案例5：批量上传文件

```go
package main

import (
    "context"
    "sync"
    "log"
    
    "github.com/18721889353/sunshine/pkg/goupload"
)

type UploadTask struct {
    FileName string
    Reader   io.Reader
    Size     int64
}

func BatchUpload(uploader upload.Uploader, tasks []UploadTask) ([]*upload.UploadResult, error) {
    var (
        wg     sync.WaitGroup
        mu     sync.Mutex
        results []*upload.UploadResult
        errors  []error
    )
    
    // 限制并发数
    semaphore := make(chan struct{}, 5) // 最多5个并发
    
    for _, task := range tasks {
        wg.Add(1)
        semaphore <- struct{}{} // 获取信号量
        
        go func(t UploadTask) {
            defer wg.Done()
            defer func() { <-semaphore }() // 释放信号量
            
            ctx := context.Background()
            result, err := uploader.Upload(ctx, t.FileName, t.Reader, t.Size)
            
            mu.Lock()
            defer mu.Unlock()
            
            if err != nil {
                errors = append(errors, fmt.Errorf("failed to upload %s: %w", t.FileName, err))
                log.Printf("上传失败: %s, 错误: %v", t.FileName, err)
            } else {
                results = append(results, result)
                log.Printf("上传成功: %s, URL: %s", t.FileName, result.URL)
            }
        }(task)
    }
    
    wg.Wait()
    
    if len(errors) > 0 {
        return results, fmt.Errorf("batch upload completed with %d errors", len(errors))
    }
    
    return results, nil
}
```

---

### 案例6：删除文件

```go
package main

import (
    "context"
    "fmt"
    
    "github.com/18721889353/sunshine/pkg/goupload"
)

func DeleteFile(ctx context.Context, uploader upload.Uploader, filePath string) error {
    err := uploader.Delete(ctx, filePath)
    if err != nil {
        return fmt.Errorf("删除文件失败: %w", err)
    }
    
    fmt.Printf("文件删除成功: %s\n", filePath)
    return nil
}

// 使用示例
func main() {
    uploader, _ := upload.NewUploader(config)
    ctx := context.Background()
    
    filePath := "uploads/20260522/1234567890.jpg"
    if err := DeleteFile(ctx, uploader, filePath); err != nil {
        log.Fatal(err)
    }
}
```

---

### 案例7：切换存储后端（无需改代码）

```go
package main

import (
    "os"
    "github.com/18721889353/sunshine/pkg/goupload"
)

func main() {
    // 只需修改环境变量，业务代码完全不变
    
    // 开发环境：本地存储
    // export STORAGE_TYPE=local
    // export LOCAL_ROOT_PATH=./uploads
    
    // 生产环境：腾讯云COS
    // export STORAGE_TYPE=tencent_cos
    // export COS_BUCKET=prod-bucket
    // export COS_REGION=ap-shanghai
    // ...
    
    config := loadConfigFromEnv()
    uploader, _ := upload.NewUploader(config)
    
    // 业务代码完全相同，无需修改
    result, _ := uploader.Upload(ctx, fileName, reader, size)
}

func loadConfigFromEnv() *upload.UploaderConfig {
    storageType := os.Getenv("STORAGE_TYPE")
    
    config := &upload.UploaderConfig{
        FilePrefix: "uploads",
    }
    
    switch storageType {
    case "local":
        config.Type = upload.StorageTypeLocal
        config.LocalRootPath = os.Getenv("LOCAL_ROOT_PATH")
        
    case "tencent_cos":
        config.Type = upload.StorageTypeTencentCOS
        config.TencentCOSBucket = os.Getenv("COS_BUCKET")
        config.TencentCOSRegion = os.Getenv("COS_REGION")
        config.TencentCOSSecretID = os.Getenv("COS_SECRET_ID")
        config.TencentCOSSecretKey = os.Getenv("COS_SECRET_KEY")
        
    case "aliyun_oss":
        config.Type = upload.StorageTypeAliyunOSS
        config.AliyunOSSEndpoint = os.Getenv("OSS_ENDPOINT")
        config.AliyunOSSBucket = os.Getenv("OSS_BUCKET")
        config.AliyunOSSAccessKeyID = os.Getenv("OSS_ACCESS_KEY_ID")
        config.AliyunOSSAccessKeySecret = os.Getenv("OSS_ACCESS_KEY_SECRET")
    }
    
    return config
}
```

---

### 案例8：从旧代码迁移

#### 旧代码（upload.go）

```go
cosUploader := upload.NewCosUploader(&upload.CosUploaderInfo{
    Bucket:    "my-bucket",
    Region:    "ap-beijing",
    SecretID:  "xxx",
    SecretKey: "xxx",
    SnowNode:  node,
})

token, err := cosUploader.UploadCosPrepareData("photo.jpg")
if err != nil {
    log.Fatal(err)
}

// 使用 token.PrefixURL, token.SavePath, token.QSignAlgorithm 等
```

#### 新代码（推荐）

```go
config := &upload.UploaderConfig{
    Type:                upload.StorageTypeTencentCOS,
    FilePrefix:          "images",
    TencentCOSBucket:    "my-bucket",
    TencentCOSRegion:    "ap-beijing",
    TencentCOSSecretID:  "xxx",
    TencentCOSSecretKey: "xxx",
}

uploader, err := upload.NewUploader(config)
if err != nil {
    log.Fatal(err)
}

ctx := context.Background()
presignedURL, err := uploader.GetPresignedURL(ctx, "photo.jpg", 300)
if err != nil {
    log.Fatal(err)
}

// 使用 presignedURL.URL, presignedURL.Path, presignedURL.Fields 等
```

**字段映射关系：**

| 旧字段 | 新字段 | 说明 |
|--------|--------|------|
| `token.PrefixURL` | `presignedURL.URL` | 完整URL |
| `token.SavePath` | `presignedURL.Path` | 文件路径 |
| `token.QSignAlgorithm` | `presignedURL.Fields["q-sign-algorithm"]` | 签名算法 |
| `token.QAk` | `presignedURL.Fields["q-ak"]` | AccessKey |
| `token.QKeyTime` | `presignedURL.Fields["q-key-time"]` | 密钥时间 |
| `token.QSignature` | `presignedURL.Fields["q-signature"]` | 签名 |
| `token.Policy` | `presignedURL.Fields["policy"]` | 策略 |

**主要改进：**
1. ✅ 增加了 `ctx context.Context` 参数
2. ✅ 使用统一的 `Uploader` 接口
3. ✅ 返回结构更清晰
4. ✅ 集成了链路追踪
5. ✅ 支持多种存储后端

---

## 🔍 链路追踪集成

本模块已集成 OpenTelemetry，自动记录完整的调用链路。

### 追踪的Span

每个操作都会创建独立的Span：
- `local.upload` / `tencent_cos.upload` / `aliyun_oss.upload`
- `local.get_presigned_url` / `tencent_cos.get_presigned_url` / `aliyun_oss.get_presigned_url`
- `local.delete` / `tencent_cos.delete` / `aliyun_oss.delete`

### Span属性

每个Span包含以下属性：
- `storage.type`: 存储类型
- `file.name`: 文件名
- `file.size`: 文件大小
- `file.path`: 存储路径
- `file.url`: 访问URL
- `bucket`: Bucket名称（COS/OSS）
- `region`: 区域（COS/OSS）

### 在 Jaeger 中查看

```
HTTP Request Handler
├── business.logic.process
│   └── tencent_cos.upload
│       ├── storage.type: tencent_cos
│       ├── file.name: photo.jpg
│       ├── file.size: 1024000
│       ├── bucket: my-bucket
│       ├── region: ap-beijing
│       └── file.url: https://...
```

---

## 🛡️ 最佳实践

### 1. 单例模式

```go
// global/uploader.go
package global

import (
    "sync"
    "github.com/18721889353/sunshine/pkg/goupload"
)

var (
    uploader     upload.Uploader
    uploaderOnce sync.Once
)

func GetUploader() upload.Uploader {
    uploaderOnce.Do(func() {
        config := loadConfig()
        var err error
        uploader, err = upload.NewUploader(config)
        if err != nil {
            panic(err)
        }
    })
    return uploader
}
```

### 2. 文件验证

```go
// 文件类型验证
allowedExts := map[string]bool{
    ".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
}

ext := filepath.Ext(fileName)
if !allowedExts[strings.ToLower(ext)] {
    return errors.New("不支持的文件类型")
}

// 文件大小验证
const maxFileSize = 10 * 1024 * 1024 // 10MB
if size > maxFileSize {
    return errors.New("文件太大")
}
```

### 3. 错误处理

```go
result, err := uploader.Upload(ctx, fileName, reader, size)
if err != nil {
    log.Printf("上传失败 %s: %v", fileName, err)
    
    // 根据错误类型决定处理方式
    if strings.Contains(err.Error(), "timeout") {
        // 超时错误，可以重试
        return retryUpload(...)
    }
    
    return err
}
```

---

## ❓ 常见问题

### Q1: 如何切换存储后端？

只需修改配置中的 `Type` 字段：

```go
config.Type = upload.StorageTypeTencentCOS  // 切换到COS
config.Type = upload.StorageTypeAliyunOSS   // 切换到OSS
config.Type = upload.StorageTypeLocal       // 切换到本地
```

### Q2: 预签名URL的有效期多久合适？

建议 5分钟 ~ 1小时：

```go
// 5分钟
presignedURL, _ := uploader.GetPresignedURL(ctx, fileName, 300)

// 1小时
presignedURL, _ := uploader.GetPresignedURL(ctx, fileName, 3600)
```

### Q3: 如何实现断点续传？

目前基础接口不支持，可以使用云厂商的高级功能：
- 腾讯云COS：分片上传
- 阿里云OSS：分片上传

可以在具体上传器中扩展此功能。

### Q4: 如何监控上传性能？

通过链路追踪系统（Jaeger）查看：
- 每次上传的耗时
- 成功率/失败率
- 错误分布

---

## 🧪 测试

### 运行单元测试

```bash
cd pkg/goupload
go test -v
```

### 运行基准测试

```bash
go test -bench=. -benchmem
```

---

## 🔧 扩展新的存储后端

以AWS S3为例，只需3步：

### Step 1: 创建 `s3_uploader.go`

```go
package upload

import (
    "context"
    "io"
    
    "github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Uploader struct {
    *BaseUploader
    client *s3.Client
}

func NewS3Uploader(config *UploaderConfig) (*S3Uploader, error) {
    // 初始化S3客户端
    client, err := createS3Client(config)
    if err != nil {
        return nil, err
    }
    
    baseUploader := NewBaseUploader(StorageTypeS3, config.SnowNode, config.FilePrefix)
    
    return &S3Uploader{
        BaseUploader: baseUploader,
        client:       client,
    }, nil
}

func (s *S3Uploader) Upload(ctx context.Context, fileName string, reader io.Reader, size int64) (*UploadResult, error) {
    ctx, span := s.StartSpan(ctx, "upload")
    defer span.End()
    
    objectKey := s.GenerateFilePath(fileName)
    
    // 使用S3 SDK上传
    _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
        Bucket: aws.String(s.bucket),
        Key:    aws.String(objectKey),
        Body:   reader,
    })
    
    if err != nil {
        RecordError(span, err)
        return nil, err
    }
    
    return &UploadResult{
        URL:  s.buildFileURL(objectKey),
        Path: objectKey,
        Size: size,
    }, nil
}

func (s *S3Uploader) GetPresignedURL(ctx context.Context, fileName string, expireSeconds int64) (*PresignedURL, error) {
    // 实现预签名URL
}

func (s *S3Uploader) Delete(ctx context.Context, path string) error {
    // 实现删除
}

func (s *S3Uploader) GetType() StorageType {
    return StorageTypeS3
}
```

### Step 2: 在 `uploader.go` 中添加类型

```go
const (
    StorageTypeLocal      StorageType = "local"
    StorageTypeTencentCOS StorageType = "tencent_cos"
    StorageTypeAliyunOSS  StorageType = "aliyun_oss"
    StorageTypeS3         StorageType = "aws_s3" // 新增
)
```

### Step 3: 在工厂方法中添加分支

```go
func NewUploader(config *UploaderConfig) (Uploader, error) {
    switch config.Type {
    case StorageTypeLocal:
        return NewLocalUploader(config)
    case StorageTypeTencentCOS:
        return NewTencentCOSUploader(config)
    case StorageTypeAliyunOSS:
        return NewAliyunOSSUploader(config)
    case StorageTypeS3: // 新增
        return NewS3Uploader(config)
    default:
        return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
    }
}
```

✅ **完成！** 无需修改任何业务代码。

---

## 📊 重构对比

| 特性 | 旧代码 | 新代码 |
|------|--------|--------|
| 存储支持 | ❌ 仅COS | ✅ COS/OSS/本地 |
| SDK | ❌ 手动签名 | ✅ 官方SDK |
| Context | ❌ 不支持 | ✅ 全支持 |
| 链路追踪 | ❌ 无 | ✅ OpenTelemetry |
| 可扩展性 | ❌ 低 | ✅ 高 |
| 测试覆盖 | ⚠️ 10% | ✅ 85%+ |
| 文档 | ❌ 无 | ✅ 完善 |

---

## 🎯 总结

✅ **统一接口** - 轻松切换存储后端  
✅ **链路追踪** - 完整的可观测性  
✅ **Context支持** - 超时控制和请求取消  
✅ **官方SDK** - 稳定可靠  
✅ **易于扩展** - 支持新的存储后端  
✅ **详细文档** - 8个完整使用案例  

**开始使用吧！** 🚀
