// Package main 演示预签名URL的使用
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/18721889353/sunshine/pkg/goupload"
)

func main() {
	fmt.Println("=== 预签名URL演示 ===")
	fmt.Println()
	fmt.Println("📖 这个示例展示如何生成预签名URL，供前端直接上传到COS")
	fmt.Println()

	// 配置腾讯云COS
	config := &goupload.UploaderConfig{
		Type:                goupload.StorageTypeTencentCOS,
		TencentCOSBucket:    "yzrzj-1318963603",
		TencentCOSRegion:    "ap-nanjing",
		TencentCOSSecretID:  "",
		TencentCOSSecretKey: "",
		TencentCOSDomain:    "https://img.yzrzj.cn",
		FilePrefix:          "apiImage",
	}

	// 创建上传器
	uploader, err := goupload.NewUploader(config)
	if err != nil {
		log.Fatalf("❌ 创建上传器失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 生成预签名URL
	fileName := "user_avatar.png"
	expireSeconds := int64(10)

	fmt.Printf("📝 请求参数:\n")
	fmt.Printf("   文件名: %s\n", fileName)
	fmt.Printf("   有效期: %d 秒 (1小时)\n", expireSeconds)
	fmt.Println()

	presignedURL, err := uploader.GetPresignedURL(ctx, fileName, expireSeconds)
	if err != nil {
		log.Fatalf("❌ 生成失败: %v", err)
	}

	fmt.Println("✅ 预签名URL生成成功!")
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("返回给前端的JSON:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("{")
	fmt.Printf("  \"uploadURL\": \"%s\",\n", presignedURL.URL)
	fmt.Printf("  \"filePath\": \"%s\",\n", presignedURL.Path)
	fmt.Printf("  \"accessURL\": \"https://img.yzrzj.cn/%s\",\n", presignedURL.Path)
	fmt.Printf("  \"expireAt\": \"%s\",\n", presignedURL.ExpireAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  \"method\": \"PUT\"\n")
	fmt.Println("}")
	fmt.Println()

	// 演示实际上传（模拟前端使用预签名URL上传）
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("【步骤2】模拟前端使用预签名URL上传文件")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// 读取本地图片文件
	filePath := "defaultAvatar.png" // 相对于当前工作目录
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("❌ 打开文件失败: %v", err)
	}
	defer file.Close()

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		log.Fatalf("❌ 获取文件信息失败: %v", err)
	}

	fmt.Printf(" 读取本地图片: %s\n", filePath)
	fmt.Printf("   文件大小: %d bytes (%.2f KB)\n", fileInfo.Size(), float64(fileInfo.Size())/1024)
	fmt.Println()
	fmt.Println(" 使用预签名URL上传 defaultAvatar.png...")

	uploadStartTime := time.Now()

	reader := file

	// 使用预签名URL上传文件（PUT请求）
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, presignedURL.URL, reader)
	if err != nil {
		log.Fatalf("❌ 创建上传请求失败: %v", err)
	}
	httpReq.Header.Set("Content-Type", "image/png")
	httpReq.Header.Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	uploadResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Fatalf("❌ 上传失败: %v", err)
	}
	//nolint:errcheck // 在程序退出时关闭response body，错误可忽略
	defer uploadResp.Body.Close()

	uploadElapsed := time.Since(uploadStartTime)

	if uploadResp.StatusCode != http.StatusOK && uploadResp.StatusCode != http.StatusCreated {
		log.Fatalf("❌ 上传失败，HTTP状态码: %d", uploadResp.StatusCode)
	}

	fmt.Printf("✅ 文件上传成功!\n")
	fmt.Printf("   ⏱️  上传耗时: %v\n", uploadElapsed)
	fmt.Println()

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("✅ 上传完成！图片可访问:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Printf("🖼️  图片地址: %s\n", presignedURL.Path)
	fmt.Printf("🔗 访问URL: https://img.yzrzj.cn/%s\n", presignedURL.Path)
	fmt.Println()
	fmt.Println("📊 完整流程总结:")
	fmt.Println("   1. ✅ 后端生成预签名URL")
	fmt.Println("   2. ✅ 前端使用预签名URL直接上传到COS")
	fmt.Println("   3. ✅ 上传完成，图片可访问")
	fmt.Println("   4. ✅ 服务器零流量消耗（仅URL传输）")
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("前端使用示例:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("// 1. 获取预签名URL")
	fmt.Println("const response = await fetch('/api/upload/presigned-url', {")
	fmt.Println("  method: 'POST',")
	fmt.Println("  body: JSON.stringify({ fileName: 'avatar.png' })")
	fmt.Println("});")
	fmt.Println("const { uploadURL, accessURL } = await response.json();")
	fmt.Println()
	fmt.Println("// 2. 直接上传到COS（不经过后端）")
	fmt.Println("await fetch(uploadURL, {")
	fmt.Println("  method: 'PUT',")
	fmt.Println("  body: file")
	fmt.Println("});")
	fmt.Println()
	fmt.Println("// 3. 完成！可以访问 accessURL 查看图片")
	fmt.Println()

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("优势对比:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("传统方式:")
	fmt.Println("  ❌ 前端 → 后端 → COS (占用服务器带宽)")
	fmt.Println("  ❌ 1GB文件 = 2GB服务器流量")
	fmt.Println()
	fmt.Println("预签名URL方式:")
	fmt.Println("  ✅ 前端 → COS (不经过后端)")
	fmt.Println("  ✅ 1GB文件 = ~2KB服务器流量")
	fmt.Println("  ✅ 节省 99.999% 服务器流量!")
	fmt.Println()
	fmt.Println("🎉 查看完整实现指南: pkg/goupload/PRESIGNED_URL_GUIDE.md")
}
