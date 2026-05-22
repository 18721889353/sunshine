// Package main 提供邮件发送测试功能
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadEnvFromFile 从.env文件加载环境变量
func loadEnvFromFile() error {
	// 获取当前可执行文件所在目录
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exeDir := filepath.Dir(exePath)

	// 尝试多个可能的.env文件位置
	envPaths := []string{
		filepath.Join(exeDir, ".env"),
		filepath.Join(".env"),
	}

	var envFile string
	for _, path := range envPaths {
		if _, statErr := os.Stat(path); statErr == nil {
			envFile = path
			break
		}
	}

	if envFile == "" {
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

// loadEnvFromSystem 从系统环境变量加载配置
func loadEnvFromSystem() (accessKeyID, secretKey string) {
	// 优先使用腾讯云专用变量
	accessKeyID = os.Getenv("TENCENT_AK")
	secretKey = os.Getenv("TENCENT_SK")

	// 如果未设置，尝试通用变量
	if accessKeyID == "" {
		accessKeyID = os.Getenv("EMAIL_AK")
	}
	if secretKey == "" {
		secretKey = os.Getenv("EMAIL_SK")
	}

	return accessKeyID, secretKey
}

// LoadConfig 加载配置（先从.env文件，再从系统环境变量）
func LoadConfig() (accessKeyID, secretKey string, err error) {
	// 首先尝试从.env文件加载
	if loadErr := loadEnvFromFile(); loadErr != nil {
		fmt.Printf("Warning: failed to load .env file: %v\n", loadErr)
	}

	// 然后从环境变量获取
	accessKeyID, secretKey = loadEnvFromSystem()
	return accessKeyID, secretKey, nil
}
