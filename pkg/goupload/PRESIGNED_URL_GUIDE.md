# 前端直传方案 - 完整实现指南

## 📖 方案说明

使用预签名URL实现前端直接上传到腾讯云COS，不经过后端服务器，大幅节省服务器带宽。

---

## 🔄 完整流程

```
┌─────────┐                    ┌──────────┐                    ┌──────────────┐
│  前端   │ ──请求URL──→    │  后端    │ ──②返回URL──→    │  前端       │
─────────┘                    └──────────┘                    └──────────────┘
                                     │                              │
                                     │                              │ ③直传文件
                                     ▼                              ▼
                              ┌──────────┐                    ┌──────────────┐
                              │ 数据库   │ ←──④记录信息──  │  腾讯云COS   │
                              └──────────                    └──────────────┘
```

---

## 1️⃣ 后端API实现（Gin框架）

### 1.1 获取预签名URL接口

```go
package handler

import (
    "fmt"
    "net/http"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/18721889353/sunshine/pkg/goupload"
)

type UploadHandler struct {
    uploader goupload.Uploader
}

func NewUploadHandler(uploader goupload.Uploader) *UploadHandler {
    return &UploadHandler{uploader: uploader}
}

// GetPresignedURL 获取预签名URL（前端直传）
// POST /api/upload/presigned-url
func (h *UploadHandler) GetPresignedURL(c *gin.Context) {
    var req struct {
        FileName      string `json:"fileName" binding:"required"`
        ExpireSeconds int64  `json:"expireSeconds"`
    }
    
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "invalid request: " + err.Error(),
        })
        return
    }
    
    // 默认1小时过期
    if req.ExpireSeconds <= 0 {
        req.ExpireSeconds = 3600
    }
    
    ctx := c.Request.Context()
    presignedURL, err := h.uploader.GetPresignedURL(ctx, req.FileName, req.ExpireSeconds)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "failed to get presigned URL: " + err.Error(),
        })
        return
    }
    
    c.JSON(http.StatusOK, gin.H{
        "uploadURL": presignedURL.URL,
        "filePath":  presignedURL.Path,
        "accessURL": fmt.Sprintf("https://img.yzrzj.cn/%s", presignedURL.Path),
        "expireAt":  presignedURL.ExpireAt.Format(time.RFC3339),
        "method":    "PUT",
    })
}

// CompleteUpload 上传完成通知（可选）
// POST /api/upload/complete
func (h *UploadHandler) CompleteUpload(c *gin.Context) {
    var req struct {
        FilePath  string `json:"filePath" binding:"required"`
        FileName  string `json:"fileName" binding:"required"`
        FileSize  int64  `json:"fileSize"`
        AccessURL string `json:"accessURL"`
    }
    
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "invalid request: " + err.Error(),
        })
        return
    }
    
    // 在这里可以：
    // 1. 保存文件信息到数据库
    // 2. 关联到用户记录
    // 3. 触发后续业务逻辑
    
    // 示例：保存到数据库
    // err := h.db.Create(&model.File{
    //     FilePath:  req.FilePath,
    //     FileName:  req.FileName,
    //     FileSize:  req.FileSize,
    //     AccessURL: req.AccessURL,
    //     UserID:    getCurrentUserID(c),
    // }).Error
    
    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "message": "upload completed",
        "data": gin.H{
            "filePath":  req.FilePath,
            "accessURL": req.AccessURL,
        },
    })
}
```

### 1.2 路由注册

```go
func registerUploadRoutes(r *gin.Engine, uploader goupload.Uploader) {
    handler := NewUploadHandler(uploader)
    
    uploadGroup := r.Group("/api/upload")
    {
        // 获取预签名URL
        uploadGroup.POST("/presigned-url", handler.GetPresignedURL)
        // 上传完成通知
        uploadGroup.POST("/complete", handler.CompleteUpload)
    }
}
```

---

## 2️⃣ 前端实现（原生JavaScript）

### 2.1 基础上传函数

```javascript
/**
 * 上传文件到腾讯云COS（前端直传）
 * @param {File} file - 要上传的文件
 * @returns {Promise<{success: boolean, accessURL: string}>}
 */
async function uploadFileToCOS(file) {
  try {
    // 步骤1: 请求后端获取预签名URL
    console.log('📡 步骤1: 获取预签名URL...');
    const presignedResponse = await fetch('/api/upload/presigned-url', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer YOUR_TOKEN' // 如果需要认证
      },
      body: JSON.stringify({
        fileName: file.name,
        expireSeconds: 3600 // URL有效期1小时
      })
    });
    
    if (!presignedResponse.ok) {
      throw new Error('Failed to get presigned URL');
    }
    
    const { uploadURL, filePath, accessURL, method } = await presignedResponse.json();
    console.log('✅ 获取预签名URL成功:', uploadURL);
    
    // 步骤2: 直接使用预签名URL上传文件到COS
    console.log('📤 步骤2: 上传文件到COS...');
    const uploadResponse = await fetch(uploadURL, {
      method: method || 'PUT',
      headers: {
        'Content-Type': file.type || 'application/octet-stream',
      },
      body: file
    });
    
    if (!uploadResponse.ok) {
      throw new Error(`Upload failed: ${uploadResponse.statusText}`);
    }
    
    console.log('✅ 文件上传成功!');
    
    // 步骤3: 通知后端上传完成（可选）
    console.log(' 步骤3: 通知后端记录文件信息...');
    const completeResponse = await fetch('/api/upload/complete', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer YOUR_TOKEN'
      },
      body: JSON.stringify({
        filePath: filePath,
        fileName: file.name,
        fileSize: file.size,
        accessURL: accessURL
      })
    });
    
    if (!completeResponse.ok) {
      console.warn('⚠️ 通知后端失败，但文件已上传成功');
    }
    
    return {
      success: true,
      accessURL: accessURL,
      filePath: filePath
    };
    
  } catch (error) {
    console.error('❌ 上传失败:', error);
    throw error;
  }
}
```

### 2.2 带进度条的上传

```javascript
/**
 * 带进度条的文件上传
 */
async function uploadFileWithProgress(file, onProgress) {
  // 步骤1: 获取预签名URL
  const presignedResponse = await fetch('/api/upload/presigned-url', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ fileName: file.name })
  });
  
  const { uploadURL, filePath, accessURL } = await presignedResponse.json();
  
  // 步骤2: 使用XMLHttpRequest上传（支持进度事件）
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    
    // 上传进度
    xhr.upload.addEventListener('progress', (event) => {
      if (event.lengthComputable) {
        const percentComplete = (event.loaded / event.total) * 100;
        onProgress(percentComplete);
      }
    });
    
    // 上传完成
    xhr.addEventListener('load', () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        // 通知后端
        fetch('/api/upload/complete', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            filePath: filePath,
            fileName: file.name,
            fileSize: file.size,
            accessURL: accessURL
          })
        });
        
        resolve({ success: true, accessURL });
      } else {
        reject(new Error('Upload failed'));
      }
    });
    
    // 上传错误
    xhr.addEventListener('error', () => {
      reject(new Error('Network error'));
    });
    
    // 开始上传
    xhr.open('PUT', uploadURL);
    xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream');
    xhr.send(file);
  });
}

// 使用示例
const fileInput = document.getElementById('fileInput');
const progressBar = document.getElementById('progressBar');

fileInput.addEventListener('change', async (e) => {
  const file = e.target.files[0];
  if (!file) return;
  
  try {
    console.log('开始上传:', file.name);
    
    const result = await uploadFileWithProgress(file, (progress) => {
      progressBar.style.width = progress + '%';
      progressBar.textContent = Math.round(progress) + '%';
    });
    
    console.log('上传成功! 访问URL:', result.accessURL);
    alert('上传成功!');
    
  } catch (error) {
    console.error('上传失败:', error);
    alert('上传失败: ' + error.message);
  }
});
```

### 2.3 React组件示例

```jsx
import React, { useState } from 'react';

function FileUploader() {
  const [uploading, setUploading] = useState(false);
  const [progress, setProgress] = useState(0);
  const [uploadedUrl, setUploadedUrl] = useState('');

  const handleUpload = async (file) => {
    setUploading(true);
    setProgress(0);
    
    try {
      const result = await uploadFileWithProgress(file, (p) => {
        setProgress(p);
      });
      
      setUploadedUrl(result.accessURL);
      console.log('上传成功:', result.accessURL);
      
    } catch (error) {
      console.error('上传失败:', error);
    } finally {
      setUploading(false);
    }
  };

  return (
    <div>
      <input
        type="file"
        onChange={(e) => e.target.files[0] && handleUpload(e.target.files[0])}
        disabled={uploading}
      />
      
      {uploading && (
        <div>
          <progress value={progress} max="100" />
          <span>{Math.round(progress)}%</span>
        </div>
      )}
      
      {uploadedUrl && (
        <div>
          <p>✅ 上传成功!</p>
          <img src={uploadedUrl} alt="Uploaded" style={{maxWidth: '300px'}} />
        </div>
      )}
    </div>
  );
}

export default FileUploader;
```

### 2.4 Vue组件示例

```vue
<template>
  <div class="file-uploader">
    <input
      type="file"
      @change="handleFileChange"
      :disabled="uploading"
    />
    
    <div v-if="uploading" class="progress-bar">
      <div 
        class="progress-fill" 
        :style="{ width: progress + '%' }"
      >
        {{ Math.round(progress) }}%
      </div>
    </div>
    
    <div v-if="uploadedUrl" class="result">
      <p>✅ 上传成功!</p>
      <img :src="uploadedUrl" alt="Uploaded" />
    </div>
  </div>
</template>

<script>
export default {
  data() {
    return {
      uploading: false,
      progress: 0,
      uploadedUrl: ''
    };
  },
  methods: {
    async handleFileChange(event) {
      const file = event.target.files[0];
      if (!file) return;
      
      this.uploading = true;
      this.progress = 0;
      
      try {
        const result = await this.uploadFileToCOS(file);
        this.uploadedUrl = result.accessURL;
      } catch (error) {
        console.error('上传失败:', error);
        alert('上传失败: ' + error.message);
      } finally {
        this.uploading = false;
      }
    },
    
    async uploadFileToCOS(file) {
      // 获取预签名URL
      const presignedRes = await fetch('/api/upload/presigned-url', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ fileName: file.name })
      });
      
      const { uploadURL, filePath, accessURL } = await presignedRes.json();
      
      // 上传文件
      const xhr = new XMLHttpRequest();
      
      return new Promise((resolve, reject) => {
        xhr.upload.addEventListener('progress', (e) => {
          if (e.lengthComputable) {
            this.progress = (e.loaded / e.total) * 100;
          }
        });
        
        xhr.addEventListener('load', () => {
          if (xhr.status >= 200 && xhr.status < 300) {
            // 通知后端
            fetch('/api/upload/complete', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                filePath,
                fileName: file.name,
                fileSize: file.size,
                accessURL
              })
            });
            
            resolve({ success: true, accessURL });
          } else {
            reject(new Error('Upload failed'));
          }
        });
        
        xhr.addEventListener('error', () => reject(new Error('Network error')));
        
        xhr.open('PUT', uploadURL);
        xhr.setRequestHeader('Content-Type', file.type);
        xhr.send(file);
      });
    }
  }
};
</script>

<style scoped>
.progress-bar {
  width: 100%;
  height: 20px;
  background-color: #f0f0f0;
  border-radius: 10px;
  overflow: hidden;
  margin: 10px 0;
}

.progress-fill {
  height: 100%;
  background-color: #4CAF50;
  transition: width 0.3s;
  text-align: center;
  color: white;
  line-height: 20px;
}

.result img {
  max-width: 300px;
  margin-top: 10px;
}
</style>
```

---

## 3️⃣ 高级功能

### 3.1 分片上传（大文件）

```javascript
/**
 * 分片上传大文件（>100MB）
 */
async function uploadLargeFile(file, chunkSize = 5 * 1024 * 1024) {
  const totalChunks = Math.ceil(file.size / chunkSize);
  const uploadId = generateUploadId(); // 生成唯一上传ID
  
  // 获取分片上传的预签名URLs
  const presignedResponse = await fetch('/api/upload/multipart/init', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      fileName: file.name,
      totalChunks: totalChunks
    })
  });
  
  const { uploadId, chunkURLs, filePath, accessURL } = await presignedResponse.json();
  
  // 上传每个分片
  const etags = [];
  for (let i = 0; i < totalChunks; i++) {
    const start = i * chunkSize;
    const end = Math.min(start + chunkSize, file.size);
    const chunk = file.slice(start, end);
    
    const response = await fetch(chunkURLs[i], {
      method: 'PUT',
      body: chunk
    });
    
    const etag = response.headers.get('ETag');
    etags.push(etag);
  }
  
  // 完成分片上传
  await fetch('/api/upload/multipart/complete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      uploadId,
      etags,
      filePath
    })
  });
  
  return { success: true, accessURL };
}
```

### 3.2 断点续传

```javascript
/**
 * 支持断点续传的上传
 */
async function uploadWithResume(file) {
  const fileKey = `upload_${file.name}_${file.size}`;
  
  // 检查是否有未完成的上传
  const cachedProgress = localStorage.getItem(fileKey);
  if (cachedProgress) {
    const { completedChunks, uploadId } = JSON.parse(cachedProgress);
    console.log('恢复上传，已完成:', completedChunks.length, '个分片');
    
    // 继续上传未完成的分片...
  }
  
  // 正常分片上传逻辑...
  // 每完成一个分片就保存到localStorage
  localStorage.setItem(fileKey, JSON.stringify({
    uploadId,
    completedChunks,
    timestamp: Date.now()
  }));
  
  // 上传完成后清除缓存
  localStorage.removeItem(fileKey);
}
```

---

## 4️⃣ 安全性考虑

### 4.1 文件类型验证

```go
// 后端验证文件类型
func (h *UploadHandler) GetPresignedURL(c *gin.Context) {
    var req struct {
        FileName string `json:"fileName" binding:"required"`
        FileType string `json:"fileType"`
    }
    
    // 验证文件扩展名
    allowedExts := map[string]bool{
        ".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
    }
    ext := filepath.Ext(req.FileName)
    if !allowedExts[strings.ToLower(ext)] {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "unsupported file type",
        })
        return
    }
    
    // 验证Content-Type
    allowedTypes := map[string]bool{
        "image/jpeg": true, "image/png": true, "image/gif": true,
    }
    if !allowedTypes[req.FileType] {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "unsupported content type",
        })
        return
    }
    
    // ... 生成预签名URL
}
```

### 4.2 文件大小限制

```go
// 限制文件大小（5MB）
const MaxFileSize = 5 * 1024 * 1024

func (h *UploadHandler) GetPresignedURL(c *gin.Context) {
    var req struct {
        FileName string `json:"fileName"`
        FileSize int64  `json:"fileSize"`
    }
    
    if req.FileSize > MaxFileSize {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "file size exceeds limit (5MB)",
        })
        return
    }
    
    // ...
}
```

### 4.3 URL有效期控制

```go
// 限制URL有效期最长为1小时
func (h *UploadHandler) GetPresignedURL(c *gin.Context) {
    var req struct {
        ExpireSeconds int64 `json:"expireSeconds"`
    }
    
    if req.ExpireSeconds <= 0 {
        req.ExpireSeconds = 300 // 默认5分钟
    }
    
    if req.ExpireSeconds > 3600 {
        req.ExpireSeconds = 3600 // 最长1小时
    }
    
    // ...
}
```

---

## 5️⃣ 性能对比

### 流量成本对比（以1GB文件为例）

| 方案 | 服务器流入流量 | 服务器流出流量 | 总计 | 成本 |
|------|--------------|--------------|------|------|
| **传统方式** | 1GB | 1GB | 2GB | 高 |
| **预签名URL** | ~1KB | ~1KB | ~2KB | 极低 |

**节省: 99.999% 服务器流量** 🎉

### 上传速度对比

| 方案 | 延迟 | 速度限制 |
|------|------|---------|
| **传统方式** | 高（经过服务器） | 受服务器带宽限制 |
| **预签名URL** | 低（直连云存储） | 仅受网络限制 |

---

## 6️⃣ 总结

### ✅ 优势

1. **节省带宽** - 不占用后端服务器流量
2. **速度快** - 前端直接上传到COS
3. **负载低** - 服务器只需处理URL生成
4. **可扩展** - 支持大文件分片上传
5. **高可用** - 减少服务器单点故障

### 📝 使用场景

- ✅ 用户头像上传
- ✅ 商品图片上传
- ✅ 文档文件上传
- ✅ 视频文件上传
- ✅ 任何大文件上传场景

### ️ 注意事项

1. 预签名URL有有效期，需要及时使用
2. 前端需要处理跨域问题（CORS配置）
3. 建议后端记录文件信息到数据库
4. 大文件建议使用分片上传
5. 做好文件类型和大小验证

---

**这就是大厂标准的文件上传方案！** 🚀
