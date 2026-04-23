// Package conf 是解析 YAML、JSON、TOML 配置文件到 Go 结构体的包。
package conf

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Parse 解析配置文件到结构体，支持 YAML、TOML、JSON 等格式，并且如果提供了 reload 函数，则开启监听配置文件变化
func Parse(configFile string, obj interface{}, reloads ...func()) error {
	// 获取配置文件的绝对路径
	confFileAbs, err := filepath.Abs(configFile)
	if err != nil {
		return err
	}

	// 分离文件路径和文件名
	filePathStr, filename := filepath.Split(confFileAbs)
	ext := strings.TrimLeft(path.Ext(filename), ".")     // 获取文件扩展名
	filename = strings.ReplaceAll(filename, "."+ext, "") // 去掉文件后缀名

	// 设置 Viper 的配置路径、文件名和文件类型
	viper.AddConfigPath(filePathStr) // 路径
	viper.SetConfigName(filename)    // 文件名
	viper.SetConfigType(ext)         // 文件类型

	// 读取配置文件
	err = viper.ReadInConfig()
	if err != nil {
		return err
	}

	// 将配置文件内容解析到结构体
	err = viper.Unmarshal(obj)
	if err != nil {
		return err
	}

	// 如果提供了 reload 函数，则开启配置文件监听
	if len(reloads) > 0 {
		watchConfig(obj, reloads...)
	}

	return nil
}

// ParseConfigData 解析数据到结构体
func ParseConfigData(data []byte, format string, obj interface{}) error {
	// 设置 Viper 的配置类型
	viper.SetConfigType(format)

	// 从字节流读取配置
	err := viper.ReadConfig(bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	// 将配置内容解析到结构体
	return viper.Unmarshal(obj)
}

// watchConfig 监听配置文件更新
func watchConfig(obj interface{}, reloads ...func()) {
	// 开启 Viper 的配置监听
	viper.WatchConfig()

	// 当配置文件发生变化时调用回调函数
	// 注意：在 Windows 上 OnConfigChange 可能会被调用两次
	viper.OnConfigChange(func(_ fsnotify.Event) {
		// 将配置文件内容解析到结构体
		err := viper.Unmarshal(obj)
		if err != nil {
			fmt.Println("viper.Unmarshal error: ", err)
		} else {
			// 调用所有提供的 reload 函数
			for _, reload := range reloads {
				reload()
			}
		}
	})
}

// Show 打印配置信息（隐藏敏感字段）
func Show(obj interface{}, fields ...string) string {
	var out string

	// 将结构体转换为 JSON 格式的字符串
	data, err := json.MarshalIndent(obj, "", "    ")
	if err != nil {
		fmt.Println("json.MarshalIndent error: ", err)
		return ""
	}

	// 逐行读取 JSON 字符串并隐藏敏感字段
	buf := bufio.NewReader(bytes.NewReader(data))
	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			break
		}
		// 添加默认的敏感字段
		fields = append(fields, `"dsn"`, `"password"`, `"pwd"`)

		out += hideSensitiveFields(line, fields...)
	}

	return out
}

// hideSensitiveFields 隐藏敏感字段
func hideSensitiveFields(line string, fields ...string) string {
	for _, field := range fields {
		if strings.Contains(line, field) {
			index := strings.Index(line, field)
			// 如果包含用户名和密码，则替换 DSN 中的密码
			if strings.Contains(line, "@") && strings.Contains(line, ":") {
				return replaceDSN(line)
			}
			// 替换敏感字段的值为 "******"
			return fmt.Sprintf("%s: \"******\",\n", line[:index+len(field)])
		}
	}

	// 如果包含用户名和密码，则替换 DSN 中的密码
	if strings.Contains(line, "@") && strings.Contains(line, ":") {
		return replaceDSN(line)
	}

	return line
}

// replaceDSN 替换 DSN 中的密码
func replaceDSN(str string) string {
	data := []byte(str)
	start, end := 0, 0
	for k, v := range data {
		if v == ':' {
			start = k
		}
		if v == '@' {
			end = k
			break
		}
	}

	if start >= end {
		return str
	}

	// 替换密码部分为 "******"
	return fmt.Sprintf("%s******%s", data[:start+1], data[end:])
}
