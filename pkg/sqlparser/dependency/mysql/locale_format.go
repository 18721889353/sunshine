package mysql

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"

	"github.com/juju/errors"
)

// parsePrecision 解析精度值
func parsePrecision(precision string) string {
	if unicode.IsDigit(rune(precision[0])) {
		for i, v := range precision {
			if unicode.IsDigit(v) {
				continue
			}
			precision = precision[:i]
			break
		}
	} else {
		precision = "0"
	}
	return precision
}

// normalizeNumber 规范化数字字符串
func normalizeNumber(number string) string {
	if number[0] == '-' && number[1] == '.' {
		number = strings.Replace(number, "-", "-0", 1)
	} else if number[0] == '.' {
		number = strings.Replace(number, ".", "0.", 1)
	}
	return number
}

// cleanNumber 清理数字字符串中的无效字符
func cleanNumber(number string) string {
	for i, v := range number {
		if unicode.IsDigit(v) {
			continue
		}
		if i == 1 && number[1] == '.' {
			continue
		}
		if v == '.' && number[1] != '.' {
			continue
		}
		number = number[:i]
		break
	}
	return number
}

// formatInvalidNumber 格式化无效数字
func formatInvalidNumber(buffer *bytes.Buffer, precision string) (string, error) {
	buffer.Write([]byte{'0'})
	position, err := strconv.ParseUint(precision, 10, 64)
	if err == nil && position > 0 {
		buffer.Write([]byte{'.'})
		buffer.WriteString(strings.Repeat("0", int(position)))
	}
	return buffer.String(), nil
}

// formatIntegerPart 格式化整数部分，添加千位分隔符
func formatIntegerPart(buffer *bytes.Buffer, integerPart string) {
	comma := []byte{','}
	pos := 0
	if len(integerPart)%3 != 0 {
		pos += len(integerPart) % 3
		buffer.WriteString(integerPart[:pos])
		buffer.Write(comma)
	}
	for ; pos < len(integerPart); pos += 3 {
		buffer.WriteString(integerPart[pos : pos+3])
		buffer.Write(comma)
	}
	buffer.Truncate(buffer.Len() - 1)
}

// appendDecimalPart 追加小数部分
func appendDecimalPart(buffer *bytes.Buffer, parts []string, position uint64) {
	if position > 0 {
		buffer.Write([]byte{'.'})
		if len(parts) == 2 {
			if uint64(len(parts[1])) >= position {
				buffer.WriteString(parts[1][:position])
			} else {
				buffer.WriteString(parts[1])
				buffer.WriteString(strings.Repeat("0", int(position)-len(parts[1])))
			}
		} else {
			buffer.WriteString(strings.Repeat("0", int(position)))
		}
	}
}

func formatENUS(number string, precision string) (string, error) {
	var buffer bytes.Buffer

	// 解析和规范化
	precision = parsePrecision(precision)
	number = normalizeNumber(number)

	// 检查是否为无效数字
	if (number[:1] == "-" && !unicode.IsDigit(rune(number[1]))) ||
		(!unicode.IsDigit(rune(number[0])) && number[:1] != "-") {
		return formatInvalidNumber(&buffer, precision)
	}

	// 处理负号
	if number[:1] == "-" {
		buffer.Write([]byte{'-'})
		number = number[1:]
	}

	// 清理数字
	number = cleanNumber(number)

	// 分割整数和小数部分
	parts := strings.Split(number, ".")

	// 格式化整数部分
	formatIntegerPart(&buffer, parts[0])

	// 处理小数部分
	position, err := strconv.ParseUint(precision, 10, 64)
	if err == nil {
		appendDecimalPart(&buffer, parts, position)
	}

	return buffer.String(), nil
}

func formatZHCN(_ string, _ string) (string, error) {
	return "", errors.New("not implemented")
}

func formatNotSupport(_ string, _ string) (string, error) {
	return "", errors.New("not support for the specific locale")
}
