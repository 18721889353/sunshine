package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/18721889353/sunshine/pkg/goupload"
)

func main() {
	fmt.Println("=== 本地 defaultAvatar.png 上传测试 ===")
	fmt.Println()

	// 检查文件是否存在
	filePath := "../defaultAvatar.png"
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Fatalf("❌ 文件不存在: %s", filePath)
	}

	// 获取文件信息
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Fatalf("❌ 获取文件信息失败: %v", err)
	}

	fmt.Printf("📁 文件信息:\n")
	fmt.Printf("   文件名: %s\n", fileInfo.Name())
	fmt.Printf("   文件大小: %d bytes (%.2f KB)\n", fileInfo.Size(), float64(fileInfo.Size())/1024)
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

	fmt.Printf("✅ 上传器创建成功\n")
	fmt.Printf("   存储类型: %s\n", uploader.GetType())
	fmt.Printf("   COS Bucket: %s\n", config.TencentCOSBucket)
	fmt.Printf("   COS Region: %s\n", config.TencentCOSRegion)
	fmt.Printf("   访问域名: %s\n", config.TencentCOSDomain)
	fmt.Println()

	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("❌ 打开文件失败: %v", err)
	}
	defer file.Close()

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("开始上传...")
	startTime := time.Now()

	// 上传文件
	result, err := uploader.Upload(ctx, fileInfo.Name(), file, fileInfo.Size())
	elapsed := time.Since(startTime)

	if err != nil {
		log.Fatalf("❌ 上传失败: %v\n   ⏱️  耗时: %v", err, elapsed)
	}

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║                   ✅ 上传成功！                          ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("📊 上传详情:\n")
	fmt.Printf("   访问URL: %s\n", result.URL)
	fmt.Printf("   存储路径: %s\n", result.Path)
	fmt.Printf("   文件大小: %d bytes\n", result.Size)
	fmt.Printf("   ⏱️  耗时: %v\n", elapsed)
	fmt.Println()
	fmt.Println("🎉 测试完成！你可以访问上面的URL查看上传的图片。")
}
