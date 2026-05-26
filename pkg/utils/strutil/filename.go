// Package strutil 提供字符串处理工具函数。
// 包括文件名安全处理、字符串格式化等实用功能。
package strutil

import (
	"errors"
	"regexp"
	"strings"
)

// ParseSafeFileName 终极清洗文件名，彻底剔除中英文空格、控制字符、中英文斜杠、括号及特殊符号。
// 如果清洗后文件名为空，则返回错误。
// 参数:
//   - rawName: 原始拼接的文件名字符串
//
// 返回:
//   - string: 清洗干净后的安全文件名
//   - error: 当名字被完全滤空时返回异常
func ParseSafeFileName(rawName string) (string, error) {
	// 1. 将特定业务分隔符（括号、斜杠、冒号、问号）优先映射为规范的单下划线
	replacer := strings.NewReplacer(
		"（", "_", "）", "_", // 中文括号
		"(", "_", ")", "_", // 英文括号
		"／", "_", "/", "_", // 中英文正斜杠
		"＼", "_", "\\", "_", // 中英文反斜杠
		"：", "_", ":", "_", // 中英文冒号
		"？", "_", "?", "_", // 中英文问号
		"“", "_", "”", "_", // 中文双引号
		"\"", "_", "'", "_", // 英文引号
	)
	mapped := replacer.Replace(rawName)

	// 2. 🔥【核心修复】基于字符（rune）的严格白名单物理清洗
	var builder strings.Builder
	for _, r := range mapped {
		// 显式白名单判断逻辑：
		switch {
		// A. 允许英文字母 (a-z, A-Z)
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			builder.WriteRune(r)
		// B. 允许数字 (0-9)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		// C. 允许标准下划线和中划线
		case r == '_' || r == '-':
			builder.WriteRune(r)
		// D. 允许汉字基本区间 (CJK Unified Ideographs)
		case r >= 0x4E00 && r <= 0x9FA5:
			builder.WriteRune(r)
		// E. 其余一切字符（不管是全角/半角空格、换行符、还是 * 等各类特殊符号）直接在此被完全吞掉抹除
		default:
			continue
		}
	}
	cleaned := builder.String()

	// 3. 美化下划线：将连续出现的多个下划线（如 "___"）合并为一个 "_"
	if multiUnderlineReg, err := regexp.Compile(`_+`); err == nil {
		cleaned = multiUnderlineReg.ReplaceAllString(cleaned, "_")
	}

	// 4. 裁剪两端：去除首尾可能由于符号转换残余的下划线或中划线
	cleaned = strings.Trim(cleaned, "_-")

	// 5. ❌ 拦截熔断：如果整个字符串全是无法识别的空格或非法字符，导致滤完为空，直接抛出异常
	if cleaned == "" {
		return "", errors.New("清洗后的文件名为空（原始名称包含太多非法字符或空格）")
	}

	return cleaned, nil
}
