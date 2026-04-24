package sql2code_test

import (
	"testing"

	_ "github.com/pingcap/tidb/pkg/parser/test_driver"

	"github.com/18721889353/sunshine/pkg/sql2code"
)

func TestGenerateModel(t *testing.T) {
	sql := `
CREATE TABLE user_example (
    id bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '用户ID',
    username varchar(64) NOT NULL DEFAULT '' COMMENT '用户名',
    email varchar(128) NOT NULL DEFAULT '' COMMENT '邮箱',
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    deleted_at datetime DEFAULT NULL COMMENT '删除时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户表示例'
`

	args := &sql2code.Args{
		SQL:      sql,
		CodeType: "model",
		JSONTag:  true,
		IsEmbed:  true,
	}

	code, err := sql2code.GenerateOne(args)
	if err != nil {
		t.Fatalf("GenerateOne failed: %v", err)
	}

	if code == "" {
		t.Fatal("Generated code is empty")
	}

	t.Logf("Generated model code:\n%s", code)
}

func TestGenerateJSON(t *testing.T) {
	sql := `
CREATE TABLE test_table (
    id bigint unsigned NOT NULL AUTO_INCREMENT,
    name varchar(100) NOT NULL DEFAULT '',
    age int NOT NULL DEFAULT 0,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='测试表'
`

	args := &sql2code.Args{
		SQL:      sql,
		CodeType: "json",
	}

	code, err := sql2code.GenerateOne(args)
	if err != nil {
		t.Fatalf("GenerateOne failed: %v", err)
	}

	if code == "" {
		t.Fatal("Generated JSON code is empty")
	}

	t.Logf("Generated JSON code:\n%s", code)
}

func TestGenerateProto(t *testing.T) {
	sql := `
CREATE TABLE product (
    id bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '产品ID',
    name varchar(200) NOT NULL DEFAULT '' COMMENT '产品名称',
    price decimal(10,2) NOT NULL DEFAULT 0.00 COMMENT '价格',
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='产品表'
`

	args := &sql2code.Args{
		SQL:        sql,
		CodeType:   "proto",
		IsWebProto: false,
	}

	code, err := sql2code.GenerateOne(args)
	if err != nil {
		t.Fatalf("GenerateOne failed: %v", err)
	}

	if code == "" {
		t.Fatal("Generated proto code is empty")
	}

	t.Logf("Generated proto code:\n%s", code)
}
