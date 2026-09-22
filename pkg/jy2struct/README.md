# jy2struct

JSON / YAML → Go 结构体代码生成库。支持自动类型推断、子结构分离、自定义标签，以及通过 YAML 注释 `# @type:` 覆盖生成类型。

## 安装

```bash
go get github.com/18721889353/sunshine/pkg/jy2struct
```

## 快速开始

```go
package main

import (
	"fmt"
	"github.com/18721889353/sunshine/pkg/jy2struct"
)

func main() {
	code, err := jy2struct.Convert(&jy2struct.Args{
		Format:    "yaml",
		InputFile: "configs/app.yml",
		Name:      "Config",
		SubStruct: true,
		Tags:      "json",
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(code)
}
```

## 参数说明

```go
type Args struct {
	Format    string // 输入格式: "json" 或 "yaml"（必填）
	Data      string // JSON/YAML 内容字符串（与 InputFile 二选一）
	InputFile string // 输入文件路径（与 Data 二选一）
	Name      string // 根结构体名称（默认 "GenerateName"）
	SubStruct bool   // 是否将嵌套 map 分离为独立结构体
	Tags      string // 额外 struct tag，多个用逗号分隔（如 "json,yaml,gorm"）
}
```

## 转换示例

### YAML → Go Struct

输入 YAML：

```yaml
server:
  name: "myapp"
  port: 8080

database:
  driver: "mysql"
  dsn: "root:pass@tcp(127.0.0.1:3306)/db"
  maxOpenConns: 20
```

生成代码（`SubStruct: true, Tags: "json"`）：

```go
type Config struct {
	Database Database `yaml:"database" json:"database"`
	Server   Server   `yaml:"server" json:"server"`
}

type Database struct {
	Driver        string `yaml:"driver" json:"driver"`
	Dsn           string `yaml:"dsn" json:"dsn"`
	MaxOpenConns  int    `yaml:"maxOpenConns" json:"maxOpenConns"`
}

type Server struct {
	Name string `yaml:"name" json:"name"`
	Port int    `yaml:"port" json:"port"`
}
```

### JSON → Go Struct

```go
code, err := jy2struct.Convert(&jy2struct.Args{
	Format:    "json",
	Data:      `{"name": "test", "count": 42}`,
	Name:      "Request",
	SubStruct: true,
	Tags:      "json",
})
```

## SubStruct 模式

`SubStruct: true` 时，嵌套 map 会被提取为独立的命名结构体：

| SubStruct | 结果 |
|-----------|------|
| `false` | 嵌套 map 内联为匿名 struct |
| `true` | 嵌套 map 提取为独立 `type Xxx struct { ... }` |

## 自定义标签

通过 `Tags` 字段添加额外的 struct tag，多个用逗号分隔：

```go
code, _ := jy2struct.Convert(&jy2struct.Args{
	Format:    "yaml",
	InputFile: "config.yml",
	Name:      "Config",
	SubStruct: true,
	Tags:      "json,yaml,gorm", // 生成三种 tag
})
```

生成结果：

```go
type Config struct {
	Host string `json:"host" yaml:"host" gorm:"host"`
	Port int    `json:"port" yaml:"port" gorm:"port"`
}
```

## `# @type:` 类型覆盖注释（YAML 专用）

### 问题背景

YAML 中的空 map（如 `headers: {}`）默认生成 `map[string]interface{}` 或提取为 `type Headers struct{}`，但实际业务可能需要 `map[string]string` 等具体类型。

### 解决方案

在 YAML 字段行末添加 `# @type:TYPE` 注释，覆盖自动生成的类型：

```yaml
otlp:
  endpoint: "localhost:4317"
  insecure: true
  headers: {}  # @type:map[string]string, OTLP 认证头部
  timeout: 10
```

生成结果：

```go
type Otlp struct {
	Endpoint string            `yaml:"endpoint" json:"endpoint"`
	Headers  map[string]string `yaml:"headers" json:"headers"`
	Insecure bool              `yaml:"insecure" json:"insecure"`
	Timeout  int               `yaml:"timeout" json:"timeout"`
}
```

### 注释格式

```
fieldName: value  # @type:Go类型, 可选描述文字
```

- `# @type:` 是固定前缀
- `Go类型` 支持任意合法 Go 类型表达式
- 逗号后的描述文字会被忽略（不会进入类型）

### 支持的类型示例

```yaml
# 基础类型
count: 0       # @type:int64
rate: 0.5      # @type:float32
enabled: true  # @type:bool

# Map 类型
headers: {}     # @type:map[string]string
metadata: {}    # @type:map[string]interface{}

# Slice 类型
tags: []        # @type:[]string
ids: []         # @type:[]int64

# 指针类型
value: ""       # @type:*string

# 空 map（无注释时的默认行为）
data: {}        # 默认生成 map[string]interface{}
data2: {}       # @type:map[string]string  覆盖后生成 map[string]string
```

### 注意事项

1. **仅对 YAML 格式有效**，JSON 格式不支持注释
2. **仅作用于当前字段**，不影响同级或嵌套字段
3. **跳过子结构生成**：有 `# @type:` 的 map 字段不会被提取为独立 struct
4. **逗号分隔**：类型表达式中不能有逗号（Go 类型本身不含逗号，不受影响）

## 空 Map 处理

| 场景 | 无 `# @type:` | 有 `# @type:` |
|------|---------------|---------------|
| `headers: {}` | `map[string]interface{}` 或提取为 `struct{}` | 按注释类型生成 |
| `data: {}` | `map[string]interface{}` | 按注释类型生成 |

## 命令行工具

### `sunshine config`

通过 `sunshine` CLI 直接转换 YAML 为 Go 结构体：

```bash
# 指定单个文件
sunshine config -f configs/app.yml

# 指定服务目录（扫描 configs/*.yml，生成到 internal/config/）
sunshine config --server-dir=.

# 指定输出目录
sunshine config -f config.yml -o ./output
```

### `make update-config`

项目 Makefile 中的快捷命令，等价于：

```bash
sunshine config --server-dir=.
```

自动扫描 `configs/` 目录下所有 `.yml` 文件，在 `internal/config/` 生成对应的 Go 结构体代码。

## 嵌套结构示例

```yaml
app:
  name: "demo"
  features:
    auth:
      enabled: true
      providers:
        - name: "github"
          clientID: ""
```

生成代码（`SubStruct: true`）：

```go
type Config struct {
	App App `yaml:"app" json:"app"`
}

type App struct {
	Features Features `yaml:"features" json:"features"`
	Name     string   `yaml:"name" json:"name"`
}

type Features struct {
	Auth Auth `yaml:"auth" json:"auth"`
}

type Auth struct {
	Enabled   bool       `yaml:"enabled" json:"enabled"`
	Providers []Providers `yaml:"providers" json:"providers"`
}

type Providers struct {
	ClientID string `yaml:"clientID" json:"clientID"`
	Name     string `yaml:"name" json:"name"`
}
```

## API 参考

### 函数

| 函数 | 说明 |
|------|------|
| `Convert(args *Args) (string, error)` | 将 JSON/YAML 转换为 Go 结构体代码 |
| `ParseJSON(input io.Reader) (interface{}, error)` | 解析 JSON |
| `ParseYaml(input io.Reader) (interface{}, error)` | 解析 YAML |
| `FmtFieldName(s string) string` | 将字段名格式化为 Go 导出名（如 `foo_id` → `FooID`） |

### 全局变量

| 变量 | 类型 | 说明 |
|------|------|------|
| `ForceFloats` | `bool` | 强制所有数字为 `float64`（默认自动区分 `int`/`float64`） |

### 类型

| 类型 | 说明 |
|------|------|
| `Args` | 转换参数结构体 |
| `Parser` | 解析器函数类型 `func(io.Reader) (interface{}, error)` |
