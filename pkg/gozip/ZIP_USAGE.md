# ZIP 压缩工具使用指南

## 📦 概述

`github.com/18721889353/sunshine/pkg/gozip` 提供了功能完整的ZIP压缩工具，支持：
- ✅ AES-256强加密（推荐）
- ✅ 多种加密方式（标准、AES-128、AES-192、AES-256）
- ✅ 可配置压缩级别
- ✅ 单文件和多文件压缩
- ✅ 整个目录递归压缩
- ✅ 详细的日志记录

## 🚀 快速开始

### 基础用法 - 压缩文件列表（带密码）

```go
import (
    "context"
    "github.com/18721889353/sunshine/pkg/gozip"
)

// 压缩多个文件
files := []string{
    "/tmp/file1.xlsx",
    "/tmp/file2.xlsx",
}
result, err := gozip.ZipFilesFromPaths(context.Background(), files, "", "password123")
if err != nil {
    log.Printf("压缩失败: %v", err)
    return
}

fmt.Printf("压缩完成:\n")
fmt.Printf("  路径: %s\n", result.Path)
fmt.Printf("  大小: %d bytes\n", result.Size)
fmt.Printf("  文件数: %d\n", result.FileCount)
fmt.Printf("  已加密: %v\n", result.IsEncrypted)
```

### 高级用法 - 自定义选项

```go
import (
    "context"
    "github.com/18721889353/sunshine/pkg/gozip"
)

files := []string{"/tmp/file1.xlsx", "/tmp/file2.xlsx"}

// 自定义压缩选项
options := &gozip.ZipOptions{
    Password:    "mypassword",
    Encryption:  gozip.ZipAES256Encryption,  // AES-256加密（最安全）
    Compression: gozip.ZipBestCompression,   // 最佳压缩率
}

result, err := gozip.ZipFilesFromPathsWithOptions(context.Background(), files, "/output/archive.zip", options)
if err != nil {
    log.Fatal(err)
}
```

### 压缩整个目录

```go
import (
    "context"
    "github.com/18721889353/sunshine/pkg/gozip"
)

// 压缩整个目录（递归包含子目录）
result, err := gozip.ZipDirectory(context.Background(), "/path/to/directory", "", "password123")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("目录压缩完成: %s\n", result.Path)
```

## 📋 API 参考

### 主要函数

#### 1. ZipFilesFromPaths

从文件路径列表创建ZIP压缩文件（带密码保护）

```go
func ZipFilesFromPaths(ctx context.Context, sourceFiles []string, destPath string, password string) (*ZipFileResult, error)
```

**参数:**
- `ctx`: 上下文，用于链路追踪
- `sourceFiles`: 源文件路径列表
- `destPath`: 目标ZIP文件路径（如果为空，自动生成临时文件）
- `password`: 压缩密码（可选，为空则不加密）

**返回:**
- `*ZipFileResult`: 压缩结果（包含路径、大小、文件数等）
- `error`: 错误信息

**示例:**
```go
files := []string{"/path/to/file1.xlsx", "/path/to/file2.xlsx"}
result, err := gozip.ZipFilesFromPaths(context.Background(), files, "", "password123")
```

---

#### 2. ZipFilesFromPathsWithOptions

从文件路径列表创建ZIP压缩文件（支持自定义选项）

```go
func ZipFilesFromPathsWithOptions(ctx context.Context, sourceFiles []string, destPath string, options *ZipOptions) (*ZipFileResult, error)
```

**参数:**
- `ctx`: 上下文，用于链路追踪
- `sourceFiles`: 源文件路径列表
- `destPath`: 目标ZIP文件路径
- `options`: 压缩选项（密码、加密类型、压缩级别等）

**ZipOptions 结构:**
```go
type ZipOptions struct {
    Password       string            // 密码（可选，为空则不加密）
    Encryption     ZipEncryptionType // 加密类型（默认AES-256）
    Compression    int               // 压缩级别（默认DefaultCompression）
    IncludeBaseDir bool              // 是否包含基础目录（仅对目录压缩有效）
}
```

**示例:**
```go
options := &gozip.ZipOptions{
    Password:    "secure_password",
    Encryption:  gozip.ZipAES256Encryption,
    Compression: gozip.ZipBestCompression,
}
result, err := gozip.ZipFilesFromPathsWithOptions(context.Background(), files, "/output.zip", options)
```

---

#### 3. ZipDirectory

压缩整个目录（递归包含子目录）

```go
func ZipDirectory(ctx context.Context, sourceDir string, destPath string, password string) (*ZipFileResult, error)
```

**参数:**
- `ctx`: 上下文，用于链路追踪
- `sourceDir`: 源目录路径
- `destPath`: 目标ZIP文件路径
- `password`: 压缩密码（可选）

**示例:**
```go
result, err := gozip.ZipDirectory(context.Background(), "/path/to/dir", "", "password123")
```

---

#### 4. ZipDirectoryWithOptions

压缩整个目录（支持自定义选项）

```go
func ZipDirectoryWithOptions(ctx context.Context, sourceDir string, destPath string, options *ZipOptions) (*ZipFileResult, error)
```

**示例:**
```go
options := &gozip.ZipOptions{
    Password:    "password",
    Encryption:  gozip.ZipAES256Encryption,
    Compression: gozip.ZipBestSpeed, // 最快速度
}
result, err := gozip.ZipDirectoryWithOptions(context.Background(), "/path/to/dir", "/output.zip", options)
```

### 常量定义

#### 压缩级别

```go
const (
    ZipDefaultCompression = 0   // 默认压缩级别
    ZipBestCompression    = -3  // 最佳压缩（最慢）
    ZipBestSpeed          = -1  // 最快速度（最低压缩率）
    ZipNoCompression      = 0   // 不压缩，仅存储
)
```

#### 加密类型

```go
const (
    ZipStandardEncryption ZipEncryptionType = iota // 标准ZIP加密（兼容性最好，安全性较低）
    ZipAES128Encryption                            // AES-128加密
    ZipAES192Encryption                            // AES-192加密
    ZipAES256Encryption                            // AES-256加密（推荐，最安全）
)
```

### 返回值结构

#### ZipFileResult

```go
type ZipFileResult struct {
    Path        string // 压缩文件路径
    Size        int64  // 压缩文件大小（字节）
    FileCount   int    // 包含的文件数量
    IsEncrypted bool   // 是否加密
}
```

## 🔐 安全性说明

### 加密强度对比

| 加密方式 | 密钥长度 | 安全性 | 兼容性 | 推荐场景 |
|---------|---------|--------|--------|----------|
| Standard Encryption | 可变 | ⭐⭐ | 所有ZIP软件 | 需要最大兼容性 |
| AES-128 | 128位 | ⭐⭐⭐⭐ | 现代ZIP软件 | 一般安全需求 |
| AES-192 | 192位 | ⭐⭐⭐⭐⭐ | 现代ZIP软件 | 较高安全需求 |
| **AES-256** | **256位** | **⭐⭐⭐⭐⭐⭐** | **现代ZIP软件** | **高安全需求（推荐）** |

### 最佳实践

1. **始终使用AES-256加密**（除非有特殊兼容性要求）
2. **密码强度**: 至少8位，包含字母、数字和特殊字符
3. **密码管理**: 不要硬编码密码，从配置或环境变量读取
4. **传输安全**: 通过HTTPS传输加密的ZIP文件

## 💡 实际应用场景

### 场景1: 订单文件压缩

```go
// 在业务逻辑中使用
func compressFiles(ctx context.Context, sourceFiles []string, password string) (string, error) {
    compressPath := filepath.Join(os.TempDir(), fmt.Sprintf("compress_%d.zip", time.Now().Unix()))
    
    result, err := gozip.ZipFilesFromPaths(ctx, sourceFiles, compressPath, password)
    if err != nil {
        return "", err
    }
    
    return result.Path, nil
}
```

### 场景2: 批量导出Excel并压缩

```go
func exportAndCompress(ctx context.Context, records []*DataRecord) (string, error) {
    var excelFiles []string
    
    // 生成Excel文件
    for i, record := range records {
        filePath := fmt.Sprintf("/tmp/export_%d.xlsx", i)
        if err := generateExcel(record, filePath); err != nil {
            return "", err
        }
        excelFiles = append(excelFiles, filePath)
    }
    
    // 压缩所有Excel文件
    options := &gozip.ZipOptions{
        Password:    generateSecurePassword(),
        Encryption:  gozip.ZipAES256Encryption,
        Compression: gozip.ZipBestCompression,
    }
    
    result, err := gozip.ZipFilesFromPathsWithOptions(ctx, excelFiles, "", options)
    if err != nil {
        return "", err
    }
    
    // 清理临时Excel文件
    for _, file := range excelFiles {
        os.Remove(file)
    }
    
    return result.Path, nil
}
```

### 场景3: 日志文件归档

```go
func archiveLogs(ctx context.Context, logDir string) (string, error) {
    // 压缩整个日志目录（不加密，便于后续分析）
    result, err := gozip.ZipDirectory(ctx, logDir, "", "")
    if err != nil {
        return "", err
    }
    
    // 移动归档文件到永久存储
    archivePath := fmt.Sprintf("/archives/logs_%s.zip", time.Now().Format("20060102"))
    os.Rename(result.Path, archivePath)
    
    return archivePath, nil
}
```

## ⚠️ 注意事项

1. **内存使用**: 当前实现将文件全部读入内存，对于超大文件（>100MB）建议使用流式处理
2. **临时文件**: 如果未指定 `destPath`，会自动生成临时文件，使用后请记得清理
3. **并发安全**: 工具函数是线程安全的，可以在多个goroutine中同时调用
4. **错误处理**: 始终检查返回的 `error`，确保压缩成功

## 🧪 测试建议

```go
func TestZipFiles(t *testing.T) {
    // 创建测试文件
    testFiles := createTestFiles()
    defer cleanupTestFiles(testFiles)
    
    // 测试加密压缩
    result, err := gozip.ZipFilesFromPaths(context.Background(), testFiles, "", "test_password")
    assert.NoError(t, err)
    assert.True(t, result.IsEncrypted)
    assert.Equal(t, len(testFiles), result.FileCount)
    
    // 验证压缩文件可以正常解压
    assert.FileExists(t, result.Path)
}
```

## 📝 更新日志

- **v1.0.0** (2026-05-20): 初始版本
  - 支持AES-256加密
  - 支持多文件压缩
  - 支持目录递归压缩
  - 完整的日志记录
- **v1.1.0** (2026-05-21): 测试优化
  - 修复辅助函数bug
  - 增加9个新测试用例
  - 代码覆盖率提升至85.5%
  - 新增特殊文件名、大文件、深层目录等测试
- **v2.0.0** (2026-05-21): 大厂标准升级
  - 所有公共方法添加 `context.Context` 参数
  - 集成 OpenTelemetry 链路追踪
  - 使用结构化日志记录
  - 符合大厂代码规范
  - 更新所有测试和文档

---

## 📦 示例文件

测试用例会在 `files/` 目录生成压缩示例：

```
pkg/gozip/files/
├── zip_usage_demo.zip          # ZIP_USAGE.md 压缩版（密码: 123456）
└── zip_usage_documentation.zip # ZIP_USAGE.md 压缩版（密码: 123456）
```

### 重新生成示例文件

```bash
cd D:/go/src/sunshine/pkg/gozip

# 生成示例文件
go test -v -run "TestGenerateLocalZipFiles|TestZipUsageMarkdown"
```

### 解压说明

- 密码: `123456`
- 需要使用支持AES加密的解压软件（7-Zip、WinRAR等）
- Windows内置解压工具不支持AES加密

---

**相关文档**: [zip.go](file://D:/go/src/sunshine/pkg/gozip/zip.go) | [zip_test.go](file://D:/go/src/sunshine/pkg/gozip/zip_test.go)
