package parser

// NullStyle 定义了处理 SQL 中 NULL 值的不同风格
type NullStyle int

// nolint 忽略某些 linter 规则
const (
	NullDisable   NullStyle = iota // 不处理 NULL 值
	NullInSql                      // 在 SQL 语句中使用 NULL
	NullInPointer                  // 使用指针来表示 NULL 值
)

// Option 是一个函数类型，用于设置选项
type Option func(*options)

// options 结构体包含了所有可配置的选项
type options struct {
	DBDriver       string            // 数据库驱动名称，例如 "mysql"
	FieldTypes     map[string]string // 字段类型映射，键为字段名，值为类型
	Charset        string            // 字符集
	Collation      string            // 排序规则
	JSONTag        bool              // 是否生成 JSON 标签
	JSONNamedType  int               // JSON 标签的命名类型，0 表示下划线命名，其他值表示驼峰命名
	TablePrefix    string            // 表名前缀
	ColumnPrefix   string            // 列名前缀
	NoNullType     bool              // 是否禁用 NULL 类型处理
	NullStyle      NullStyle         // 处理 NULL 值的风格
	Package        string            // 生成代码的包名
	GormType       bool              // 是否在 GORM 标签中写入类型
	ForceTableName bool              // 是否强制使用表名
	IsEmbed        bool              // 是否嵌入 gorm.Model
	IsWebProto     bool              // 是否生成包含路由路径和 Swagger 信息的 proto 文件
	IsExtendedAPI  bool              // 是否生成扩展 API（9 个 API），否则生成基本 API（5 个 API）

	IsCustomTemplate bool // 是否使用自定义扩展模板，否则使用默认模板
}

// defaultOptions 定义了默认的选项配置
var defaultOptions = options{
	DBDriver:   "mysql",             // 默认数据库驱动为 mysql
	FieldTypes: map[string]string{}, // 默认字段类型映射为空
	NullStyle:  NullInSql,           // 默认处理 NULL 值的风格为在 SQL 语句中使用 NULL
	Package:    "model",             // 默认生成代码的包名为 model
}

// WithDBDriver 设置数据库驱动
func WithDBDriver(driver string) Option {
	return func(o *options) {
		if driver != "" {
			o.DBDriver = driver // 如果传入的驱动不为空，则更新 DBDriver
		}
	}
}

// WithFieldTypes 设置字段类型映射
func WithFieldTypes(fieldTypes map[string]string) Option {
	return func(o *options) {
		if fieldTypes != nil {
			o.FieldTypes = fieldTypes // 如果传入的字段类型映射不为空，则更新 FieldTypes
		}
	}
}

// WithCharset 设置字符集
func WithCharset(charset string) Option {
	return func(o *options) {
		o.Charset = charset // 更新 Charset
	}
}

// WithCollation 设置排序规则
func WithCollation(collation string) Option {
	return func(o *options) {
		o.Collation = collation // 更新 Collation
	}
}

// WithTablePrefix 设置表名前缀
func WithTablePrefix(p string) Option {
	return func(o *options) {
		o.TablePrefix = p // 更新 TablePrefix
	}
}

// WithColumnPrefix 设置列名前缀
func WithColumnPrefix(p string) Option {
	return func(o *options) {
		o.ColumnPrefix = p // 更新 ColumnPrefix
	}
}

// WithJSONTag 设置是否生成 JSON 标签以及命名类型
func WithJSONTag(namedType int) Option {
	return func(o *options) {
		o.JSONTag = true            // 启用 JSON 标签
		o.JSONNamedType = namedType // 设置 JSON 标签的命名类型
	}
}

// WithNoNullType 设置是否禁用 NULL 类型处理
func WithNoNullType() Option {
	return func(o *options) {
		o.NoNullType = true // 启用 NoNullType
	}
}

// WithNullStyle 设置处理 NULL 值的风格
func WithNullStyle(s NullStyle) Option {
	return func(o *options) {
		o.NullStyle = s // 更新 NullStyle
	}
}

// WithPackage 设置生成代码的包名
func WithPackage(pkg string) Option {
	return func(o *options) {
		o.Package = pkg // 更新 Package
	}
}

// WithGormType 设置是否在 GORM 标签中写入类型
func WithGormType() Option {
	return func(o *options) {
		o.GormType = true // 启用 GormType
	}
}

// WithForceTableName 设置是否强制使用表名
func WithForceTableName() Option {
	return func(o *options) {
		o.ForceTableName = true // 启用 ForceTableName
	}
}

// WithEmbed 设置是否嵌入 gorm.Model
func WithEmbed() Option {
	return func(o *options) {
		o.IsEmbed = true // 启用 IsEmbed
	}
}

// WithWebProto 设置是否生成包含路由路径和 Swagger 信息的 proto 文件
func WithWebProto() Option {
	return func(o *options) {
		o.IsWebProto = true // 启用 IsWebProto
	}
}

// WithExtendedAPI 设置是否生成扩展 API
func WithExtendedAPI() Option {
	return func(o *options) {
		o.IsExtendedAPI = true // 启用 IsExtendedAPI
	}
}

// WithCustomTemplate 设置是否使用自定义扩展模板
func WithCustomTemplate() Option {
	return func(o *options) {
		o.IsCustomTemplate = true // 启用 IsCustomTemplate
	}
}

// parseOption 解析并应用传入的选项
func parseOption(options []Option) options {
	o := defaultOptions // 初始化选项为默认值
	for _, f := range options {
		f(&o) // 应用每个选项
	}
	if o.NoNullType {
		o.NullStyle = NullDisable // 如果启用了 NoNullType，则将 NullStyle 设置为 NullDisable
	}
	return o // 返回解析后的选项
}
