// Package jy2struct is a library for generating go struct code, supporting json and yaml.
package jy2struct

import (
	"bytes"
	"errors"
	"os"
	"strings"
)

// Args 转换参数，定义输入格式、内容和输出结构体名称
//
// 字段说明：
//   Format    - 文档格式，支持 "json" 或 "yaml"
//   Data      - JSON 或 YAML 内容（与 InputFile 二选一）
//   InputFile - 输入文件路径（与 Data 二选一）
//   Name      - 生成的根结构体名称
//   SubStruct - 是否将子结构分离为独立结构体
//   Tags      - 额外的标签，多个标签用逗号分隔
type Args struct {
	Format    string
	Data      string
	InputFile string
	Name      string
	SubStruct bool
	Tags      string

	tags          []string //nolint
	convertFloats bool
	parser        Parser
}

// checkValid 校验并初始化转换参数
//
// 校验 Format 是否为支持的格式，初始化 parser 和 tags。
// 如果 Name 为空，默认设为 "GenerateName"。
//
// 返回值：
//   error - 如果 Format 不是 json 或 yaml 则返回错误
func (j *Args) checkValid() error {
	switch j.Format {
	case "json":
		j.parser = ParseJSON
		j.convertFloats = true
	case "yaml":
		j.parser = ParseYaml
	default:
		return errors.New("format must be json or yaml")
	}

	j.tags = []string{j.Format}
	tags := strings.Split(j.Tags, ",")
	for _, tag := range tags {
		if tag == j.Format || tag == "" {
			continue
		}
		j.tags = append(j.tags, tag)
	}

	if j.Name == "" {
		j.Name = "GenerateName"
	}

	return nil
}

// Convert 将 JSON 或 YAML 内容转换为 Go 结构体代码
//
// 优先使用 Data 字段的内容，如果 Data 为空则从 InputFile 读取文件。
//
// 参数：
//   args - 转换参数，包含格式、数据源、结构体名称等配置
// 返回值：
//   string - 生成的 Go 结构体代码
//   error  - 如果参数校验失败、文件读取失败或转换失败则返回错误
func Convert(args *Args) (string, error) {
	err := args.checkValid()
	if err != nil {
		return "", err
	}

	var data []byte
	if args.Data != "" {
		data = []byte(args.Data)
	} else {
		data, err = os.ReadFile(args.InputFile)
		if err != nil {
			return "", err
		}
	}

	input := bytes.NewReader(data)

	output, err := jyParse(input, args.parser, args.Name, "main", args.tags, args.SubStruct, args.convertFloats)
	if err != nil {
		return "", err
	}

	return string(output), nil
}
