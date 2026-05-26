// Package main 提供短信发送测试功能
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// loadEnvFromFile 从.env文件加载环境变量
func loadEnvFromFile() error {
	// 获取当前工作目录
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	fmt.Printf("📂 当前工作目录: %s\n", workDir)

	// 尝试多个可能的.env文件位置
	envPaths := []string{
		filepath.Join(workDir, ".env"),
		filepath.Join(".env"),
	}

	var envFile string
	for _, path := range envPaths {
		fmt.Printf("🔍 检查文件: %s ... ", path)
		if _, statErr := os.Stat(path); statErr == nil {
			envFile = path
			fmt.Println("✅ 找到")
			break
		} else {
			fmt.Println("❌ 不存在")
		}
	}

	if envFile == "" {
		fmt.Println("⚠️  .env文件不存在，将使用系统环境变量")
		return nil // .env文件不存在，使用系统环境变量
	}

	file, err := os.Open(envFile)
	if err != nil {
		return fmt.Errorf("failed to open .env file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 解析 KEY=VALUE 格式
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			fmt.Printf("Warning: invalid format at line %d: %s\n", lineNum, line)
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// 移除引号（如果存在）
		value = strings.Trim(value, "\"'")

		// 设置环境变量（仅当未设置时）
		if os.Getenv(key) == "" {
			//nolint:errcheck // 设置环境变量失败不影响主流程
			os.Setenv(key, value)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading .env file: %w", err)
	}

	return nil
}

// LoadConfig 加载配置（先从.env文件，再从系统环境变量）
func LoadConfig() (secretID, secretKey, appID, signName, templateID, phoneNumber string, err error) {
	// 首先尝试从.env文件加载
	if loadErr := loadEnvFromFile(); loadErr != nil {
		fmt.Printf("Warning: failed to load .env file: %v\n", loadErr)
	}

	// 然后从环境变量获取
	secretID = os.Getenv("TENCENT_SECRET_ID")
	secretKey = os.Getenv("TENCENT_SECRET_KEY")
	appID = os.Getenv("TENCENT_SMS_APP_ID")
	signName = os.Getenv("SMS_SIGN_NAME")
	templateID = os.Getenv("SMS_TEMPLATE_ID")
	phoneNumber = os.Getenv("TEST_PHONE_NUMBER")

	// 如果没有设置测试手机号，使用默认值
	if phoneNumber == "" {
		phoneNumber = "+8613711112222"
	}

	// 调试信息：显示是否加载到了配置
	if secretID == "" {
		fmt.Println("⚠️  警告: 未找到 TENCENT_SECRET_ID")
	} else {
		fmt.Printf("✅ 已加载 TENCENT_SECRET_ID: %s...\n", secretID[:10])
	}

	return secretID, secretKey, appID, signName, templateID, phoneNumber, nil
}

// MustAtoi 将字符串转换为整数，失败返回默认值
func MustAtoi(s string, defaultValue int) int {
	if s == "" {
		return defaultValue
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return defaultValue
	}
	return i
}
