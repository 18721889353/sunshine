package utils

import (
	"fmt"
	"net/url"
	"strings"
)

// AdaptiveMysqlDsn 适应各种 MySQL 格式的 DSN 地址
// 将 "mysql://" 前缀替换为空字符串
func AdaptiveMysqlDsn(dsn string) string {
	return strings.ReplaceAll(dsn, "mysql://", "")
}

// AdaptivePostgresqlDsn 将 PostgreSQL 的 DSN 转换为键值对字符串
// 如果 DSN 中包含空格超过 3 个，则直接返回原字符串
// 如果 DSN 不以 "postgres://" 开头，则添加前缀
// 删除 DSN 中的括号
// 解析 DSN 并设置默认的 sslmode 为 disable
// 返回格式化的键值对字符串
func AdaptivePostgresqlDsn(dsn string) string {
	if strings.Count(dsn, " ") > 3 {
		return dsn
	}

	if !strings.Contains(dsn, "postgres://") {
		dsn = "postgres://" + dsn
	}

	dsn = DeleteBrackets(dsn)

	u, err := url.Parse(dsn)
	if err != nil {
		panic(err)
	}

	password, _ := u.User.Password()

	if u.RawQuery == "" {
		u.RawQuery = "sslmode=disable"
	} else if u.Query().Get("sslmode") == "" {
		u.RawQuery = "sslmode=disable&" + u.RawQuery
	}
	ss := strings.Split(u.RawQuery, "&")

	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s %s",
		u.Hostname(), u.Port(), u.User.Username(), password, u.Path[1:], strings.Join(ss, " "))
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
