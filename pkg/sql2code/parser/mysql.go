package parser

import (
	"database/sql" // 导入数据库操作包
	"fmt"          // 导入格式化输入输出包

	_ "github.com/go-sql-driver/mysql" // 导入 MySQL 驱动，_ 表示仅导入而不使用
)

// GetMysqlTableInfo 从 MySQL 数据库中获取表的创建信息
func GetMysqlTableInfo(dsn, tableName string) (string, error) {
	// 打开数据库连接
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return "", fmt.Errorf("GetMysqlTableInfo 错误, %v", err)
	}
	defer db.Close() // 关闭数据库连接

	// 执行 SQL 查询，获取表的创建信息
	rows, err := db.Query("SHOW CREATE TABLE `" + tableName + "`")
	if err != nil {
		return "", fmt.Errorf("查询 SHOW CREATE TABLE 错误, %v", err)
	}

	defer rows.Close() // 关闭查询结果集

	// 检查查询结果是否为空
	if !rows.Next() {
		return "", fmt.Errorf("未找到表 '%s'", tableName)
	}

	// 读取查询结果
	var table string
	var info string
	err = rows.Scan(&table, &info)
	if err != nil {
		return "", err
	}

	return info, nil
}

// GetTableInfo 从 MySQL 数据库中获取表的创建信息
// 已废弃：请使用 GetMysqlTableInfo 替代
func GetTableInfo(dsn, tableName string) (string, error) {
	return GetMysqlTableInfo(dsn, tableName)
}
