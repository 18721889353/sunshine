// Package sql2code 提供根据 SQL 生成不同用途代码的功能，
// 支持生成 JSON、GORM 模型、更新参数、请求参数代码，
// SQL 可以从参数、文件、数据库三种方式获取，优先级从高到低。
package sql2code

import (
	"errors"  // 导入错误处理包
	"fmt"     // 导入格式化输入输出包
	"os"      // 导入操作系统包
	"strings" // 导入字符串处理包

	"github.com/18721889353/sunshine/pkg/sql2code/parser" // 导入 SQL 解析器包
	"github.com/18721889353/sunshine/pkg/utils"           // 导入工具包
)

// Args 生成代码的参数结构体
type Args struct {
	SQL string // DDL SQL 语句

	DDLFile string // DDL 文件路径

	DBDriver   string            // 数据库驱动名称，如 mysql, postgresql，默认为 mysql
	DBDsn      string            // 连接数据库的 DSN
	DBTable    string            // 表名
	fieldTypes map[string]string // 字段名:类型映射

	Package        string // 指定包名（仅对模型类型有效）
	GormType       bool   // 是否显示 GORM 类型名称（仅对模型类型代码有效）
	JSONTag        bool   // 是否包含 JSON 标签
	JSONNamedType  int    // JSON 字段命名类型，0: 蛇形命名如 my_field_name，1: 驼峰命名如 myFieldName
	IsEmbed        bool   // 是否嵌入 gorm.Model
	IsWebProto     bool   // proto 文件类型，true: 包含路由路径和 Swagger 信息，false: 正常 proto 文件不包含路由和 Swagger
	CodeType       string // 指定生成的不同类型的代码，包括 model（默认）、json、dao、handler、proto
	ForceTableName bool
	Charset        string
	Collation      string
	TablePrefix    string
	ColumnPrefix   string
	NoNullType     bool
	NullStyle      string
	IsExtendedAPI  bool // true: 生成扩展 API（9 个 API），false: 生成基本 API（5 个 API）

	IsCustomTemplate bool // 是否使用自定义模板，默认为 false
}

// checkValid 检查 Args 结构体的有效性
func (a *Args) checkValid() error {
	// 必须指定 SQL 或 DDL 文件
	if a.SQL == "" && a.DDLFile == "" && (a.DBDsn == "" && a.DBTable == "") {
		return errors.New("必须指定 SQL 或 DDL 文件")
	}
	// 检查表名是否以 _test 结尾
	if a.DBTable != "" {
		tables := strings.Split(a.DBTable, ",")
		for _, name := range tables {
			if strings.HasSuffix(name, "_test") {
				return fmt.Errorf(`表名 (%s) 后缀 "_test" 不支持代码生成，请删除后缀 "_test" 或更改为其他名称。`, name)
			}
		}
	}

	// 设置默认数据库驱动为 MySQL
	if a.DBDriver == "" {
		a.DBDriver = parser.DBDriverMysql
	}
	// 如果未指定字段类型映射，则初始化为空映射
	if a.fieldTypes == nil {
		a.fieldTypes = make(map[string]string)
	}
	return nil
}

// getSQL 获取 SQL 语句
func getSQL(args *Args) (string, map[string]string, error) {
	if args.SQL != "" {
		return args.SQL, nil, nil
	}

	sql := ""
	dbDriverName := strings.ToLower(args.DBDriver)
	if args.DDLFile != "" {
		// 只支持 MySQL DDL 文件
		if dbDriverName != parser.DBDriverMysql {
			return sql, nil, fmt.Errorf("不支持使用 %s 解析 SQL 文件，仅支持 MySQL", args.DBDriver)
		}
		b, err := os.ReadFile(args.DDLFile)
		if err != nil {
			return sql, nil, fmt.Errorf("读取 %s 失败，%s", args.DDLFile, err)
		}
		return string(b), nil, nil
	} else if args.DBDsn != "" {
		if args.DBTable == "" {
			return sql, nil, errors.New("缺少数据库表名")
		}

		switch dbDriverName {
		case parser.DBDriverMysql, parser.DBDriverTidb:
			dsn := utils.AdaptiveMysqlDsn(args.DBDsn)
			sqlStr, err := parser.GetMysqlTableInfo(dsn, args.DBTable)
			return sqlStr, nil, err
		case parser.DBDriverPostgresql:
			dsn := utils.AdaptivePostgresqlDsn(args.DBDsn)
			fields, err := parser.GetPostgresqlTableInfo(dsn, args.DBTable)
			if err != nil {
				return "", nil, err
			}
			sqlStr, pgTypeMap := parser.ConvertToSQLByPgFields(args.DBTable, fields)
			return sqlStr, pgTypeMap, nil
		default:
			return "", nil, errors.New("获取 SQL 错误，不支持的数据库驱动: " + dbDriverName)
		}
	}

	return sql, nil, errors.New("没有 SQL 输入(-sql|-f|-db-dsn)")
}

// setOptions 设置解析器选项
func setOptions(args *Args) []parser.Option {
	var opts []parser.Option

	if args.DBDriver != "" {
		opts = append(opts, parser.WithDBDriver(args.DBDriver))
	}
	if args.fieldTypes != nil {
		opts = append(opts, parser.WithFieldTypes(args.fieldTypes))
	}

	if args.Charset != "" {
		opts = append(opts, parser.WithCharset(args.Charset))
	}
	if args.Collation != "" {
		opts = append(opts, parser.WithCollation(args.Collation))
	}
	if args.JSONTag {
		opts = append(opts, parser.WithJSONTag(args.JSONNamedType))
	}
	if args.TablePrefix != "" {
		opts = append(opts, parser.WithTablePrefix(args.TablePrefix))
	}
	if args.ColumnPrefix != "" {
		opts = append(opts, parser.WithColumnPrefix(args.ColumnPrefix))
	}
	if args.NoNullType {
		opts = append(opts, parser.WithNoNullType())
	}
	if args.IsEmbed {
		opts = append(opts, parser.WithEmbed())
	}
	if args.IsWebProto {
		opts = append(opts, parser.WithWebProto())
	}

	if args.NullStyle != "" {
		switch args.NullStyle {
		case "sql":
			opts = append(opts, parser.WithNullStyle(parser.NullInSql))
		case "ptr":
			opts = append(opts, parser.WithNullStyle(parser.NullInPointer))
		default:
			fmt.Printf("无效的 null 样式: %s\n", args.NullStyle)
			return nil
		}
	} else {
		opts = append(opts, parser.WithNullStyle(parser.NullDisable))
	}
	if args.Package != "" {
		opts = append(opts, parser.WithPackage(args.Package))
	}
	if args.GormType {
		opts = append(opts, parser.WithGormType())
	}
	if args.ForceTableName {
		opts = append(opts, parser.WithForceTableName())
	}
	if args.IsExtendedAPI {
		opts = append(opts, parser.WithExtendedAPI())
	}
	if args.IsCustomTemplate {
		opts = append(opts, parser.WithCustomTemplate())
	}

	return opts
}

// GenerateOne 从 SQL 生成 GORM 代码，SQL 可以从参数、文件、数据库获取，优先级从高到低
func GenerateOne(args *Args) (string, error) {
	codes, err := Generate(args)
	if err != nil {
		return "", err
	}

	if args.CodeType == "" {
		args.CodeType = parser.CodeTypeModel // 默认生成模型代码
	}
	out, ok := codes[args.CodeType]
	if !ok {
		return "", fmt.Errorf("未知的代码类型 %s", args.CodeType)
	}

	return out, nil
}

// Generate 生成模型、JSON、DAO、Handler、Proto 代码
func Generate(args *Args) (map[string]string, error) {
	if err := args.checkValid(); err != nil {
		return nil, err
	}

	sql, fieldTypes, err := getSQL(args)
	if err != nil {
		return nil, err
	}
	if fieldTypes != nil {
		args.fieldTypes = fieldTypes
	}
	if sql == "" {
		return nil, fmt.Errorf("从 %s 获取 SQL 错误，可能是表 %s 不存在", args.DBDriver, args.DBTable)
	}

	opt := setOptions(args)

	return parser.ParseSQL(sql, opt...)
}
