package utils

import (
	"strings"
)

// AdaptiveMysqlDsn 适应各种 MySQL 格式的 DSN 地址
// 将 "mysql://" 前缀替换为空字符串
func AdaptiveMysqlDsn(dsn string) string {
	return strings.ReplaceAll(dsn, "mysql://", "")
}

// DeleteBrackets 删除 DSN 中的括号
// 查找并删除形如 "@(host:port)/" 的括号
func DeleteBrackets(str string) string {
	start := strings.Index(str, "@(")
	end := strings.LastIndex(str, ")/")

	if start == -1 || end == -1 {
		return str
	}

	addr := str[start+2 : end]
	return strings.Replace(str, "@("+addr+")/", "@"+addr+"/", 1)
}
