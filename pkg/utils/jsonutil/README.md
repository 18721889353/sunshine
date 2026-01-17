# jsonutil 包使用文档

## 简介

jsonutil 是一个基于 [json-iterator](https://github.com/json-iterator/go) 的高性能 JSON 工具包，提供了优化的序列化和反序列化功能。该包使用 Go 泛型消除 interface{} 导致的逃逸分配，同时支持灵活的配置选项。

## 特性

- 使用泛型优化内存分配，提升性能
- 支持自定义配置选项
- 提供缓存机制，避免重复创建 API 实例
- 兼容标准库接口
- 支持带验证的序列化和反序列化

## 快速开始

```go
package main

import (
    "fmt"
    "sunshine/pkg/utils/jsonutil"
)

type User struct {
    ID       int64            `json:"id"`
    Username string           `json:"username"`
    Email    string           `json:"email"`
    Active   bool             `json:"active"`
    Tags     []string         `json:"tags"`
    Meta     map[string]int64 `json:"meta"`
}

func main() {
    user := User{
        ID:       1,
        Username: "testuser",
        Email:    "test@example.com",
        Active:   true,
        Tags:     []string{"tag1", "tag2"},
        Meta:     map[string]int64{"key": 123},
    }

    // 序列化
    data, err := jsonutil.Marshal(user)
    if err != nil {
        panic(err)
    }
    
    fmt.Println(string(data))

    // 反序列化
    var unmarshaledUser User
    err = jsonutil.Unmarshal(data, &unmarshaledUser)
    if err != nil {
        panic(err)
    }

    fmt.Printf("%+v\n", unmarshaledUser)
}
```

## 配置选项

### Config 结构体

```go
type Config struct {
    EscapeHTML             bool  // 是否转义 HTML 字符，默认 false
    SortMapKeys            bool  // 是否对 Map 键排序，默认 false  
    ValidateJsonRawMessage bool  // 是否验证 JSON Raw Message，默认 false
}
```

### 默认配置

默认配置优化了性能，设置如下：
- `EscapeHTML`: false (不转义HTML，提高性能)
- `SortMapKeys`: false (不对map key排序，提高性能)
- `ValidateJsonRawMessage`: false (不自动验证JSON Raw Message，提高性能)

## API 接口

### 序列化函数

#### Marshal
将 Go 对象序列化为 JSON 字节数组。

```go
func Marshal[T any](v T) ([]byte, error)
```

示例：
```go
data, err := jsonutil.Marshal(myStruct)
if err != nil {
    // 处理错误
}
```

#### MarshalToWriter
将 Go 对象序列化并写入 io.Writer。

```go
func MarshalToWriter[T any](w io.Writer, v T) error
```

示例：
```go
err := jsonutil.MarshalToWriter(os.Stdout, myStruct)
if err != nil {
    // 处理错误
}
```

#### MarshalIndent
将 Go 对象序列化为带缩进的 JSON 字节数组。

```go
func MarshalIndent[T any](v T, prefix, indent string) ([]byte, error)
```

示例：
```go
data, err := jsonutil.MarshalIndent(myStruct, "", "  ")
if err != nil {
    // 处理错误
}
```

#### MarshalWithConfig
使用指定配置进行序列化。

```go
func MarshalWithConfig[T any](v T, cfg Config) ([]byte, error)
```

示例：
```go
cfg := jsonutil.Config{
    EscapeHTML: true,
    SortMapKeys: true,
}
data, err := jsonutil.MarshalWithConfig(myStruct, cfg)
if err != nil {
    // 处理错误
}
```

### 反序列化函数

#### Unmarshal
将 JSON 字节数组反序列化为 Go 对象。

```go
func Unmarshal[T any](data []byte, v *T) error
```

示例：
```go
var myStruct MyStruct
err := jsonutil.Unmarshal(jsonBytes, &myStruct)
if err != nil {
    // 处理错误
}
```

#### UnmarshalWithValidation
带验证的反序列化，不允许未知字段。

```go
func UnmarshalWithValidation[T any](data []byte, v *T) error
```

示例：
```go
var myStruct MyStruct
err := jsonutil.UnmarshalWithValidation(jsonBytes, &myStruct)
if err != nil {
    // 处理错误，包括未知字段错误
}
```

### 工具函数

#### IsValid
检查 JSON 数据是否有效。

```go
func IsValid(data []byte) bool
```

示例：
```go
if jsonutil.IsValid(jsonBytes) {
    // JSON 有效
} else {
    // JSON 无效
}
```

#### GetDecoder / GetEncoder
获取预配置的解码器/编码器。

```go
func GetDecoder(r io.Reader) *jsoniter.Decoder
func GetEncoder(w io.Writer) *jsoniter.Encoder
```

示例：
```go
decoder := jsonutil.GetDecoder(reader)
var obj MyStruct
err := decoder.Decode(&obj)
```

### 配置管理函数

#### SetConfig
设置全局配置。

```go
func SetConfig(cfg Config)
```

示例：
```go
cfg := jsonutil.Config{
    EscapeHTML: true,
    SortMapKeys: false,
}
jsonutil.SetConfig(cfg)
```

#### GetDefaultConfig
获取默认配置。

```go
func GetDefaultConfig() Config
```

示例：
```go
defaultCfg := jsonutil.GetDefaultConfig()
```

#### NewAPIWithConfig
创建新的 API 实例，使用指定配置。

```go
func NewAPIWithConfig(cfg Config) jsoniter.API
```

示例：
```go
api := jsonutil.NewAPIWithConfig(cfg)
// 使用自定义 API 进行操作
```

### 兼容标准库的函数

#### MarshalStd / UnmarshalStd
兼容标准库的序列化和反序列化函数。

```go
func MarshalStd(v interface{}) ([]byte, error)
func UnmarshalStd(data []byte, v interface{}) error
```

这些函数提供与标准库完全相同的接口。

## 性能优势

1. **泛型优化**: 使用 Go 泛型消除 interface{} 导致的逃逸分配
2. **实例缓存**: 缓存不同配置的 API 实例，避免重复创建
3. **读写器优化**: 提供直接写入 io.Writer 的函数，减少内存分配
4. **并发安全**: 所有操作都是并发安全的，适合高并发场景

## 使用建议

1. **一般用途**: 直接使用 [Marshal](file:///D:/go/src/sun/sunshine/pkg/utils/jsonutil/jsonutil.go#L97-L100) 和 [Unmarshal](file:///D:/go/src/sun/sunshine/pkg/utils/jsonutil/jsonutil.go#L106-L108) 函数
2. **写入流**: 需要直接写入文件或网络连接时，使用 [MarshalToWriter](file:///D:/go/src/sun/sunshine/pkg/utils/jsonutil/jsonutil.go#L93-L96)
3. **格式化输出**: 需要格式化的 JSON 输出时，使用 [MarshalIndent](file:///D:/go/src/sun/sunshine/pkg/utils/jsonutil/jsonutil.go#L140-L155)
4. **严格解析**: 需要验证未知字段时，使用 [UnmarshalWithValidation](file:///D:/go/src/sun/sunshine/pkg/utils/jsonutil/jsonutil.go#L124-L137)
5. **自定义配置**: 根据需求调整 [Config](file:///D:/go/src/sun/sunshine/pkg/utils/jsonutil/jsonutil.go#L14-L18) 选项

## 示例

### 基本用法

```go
package main

import (
    "fmt"
    "sunshine/pkg/utils/jsonutil"
)

func main() {
    // 序列化基本类型
    data, _ := jsonutil.Marshal("hello world")
    fmt.Println(string(data)) // "hello world"

    // 序列化结构体
    type Person struct {
        Name string `json:"name"`
        Age  int    `json:"age"`
    }
    
    person := Person{Name: "Alice", Age: 30}
    data, _ = jsonutil.Marshal(person)
    fmt.Println(string(data)) // {"name":"Alice","age":30}

    // 反序列化
    var newPerson Person
    jsonutil.Unmarshal(data, &newPerson)
    fmt.Printf("%+v\n", newPerson) // {Name:Alice Age:30}
}
```

### 自定义配置

```go
package main

import (
    "fmt"
    "sunshine/pkg/utils/jsonutil"
)

func main() {
    type Data struct {
        Script string `json:"script"`
    }
    
    data := Data{Script: "<script>alert('xss')</script>"}
    
    // 使用转义HTML的配置
    cfg := jsonutil.Config{EscapeHTML: true}
    escapedData, _ := jsonutil.MarshalWithConfig(data, cfg)
    fmt.Println("Escaped:", string(escapedData))
    
    // 使用不转义的配置
    cfgNoEscape := jsonutil.Config{EscapeHTML: false}
    normalData, _ := jsonutil.MarshalWithConfig(data, cfgNoEscape)
    fmt.Println("Normal:", string(normalData))
}
```

### 写入文件

```go
package main

import (
    "os"
    "sunshine/pkg/utils/jsonutil"
)

func main() {
    type Config struct {
        Port int    `json:"port"`
        Host string `json:"host"`
    }
    
    config := Config{Port: 8080, Host: "localhost"}
    
    file, _ := os.Create("config.json")
    defer file.Close()
    
    // 直接写入文件
    _ = jsonutil.MarshalToWriter(file, config)
}
```

## 注意事项

1. 所有泛型函数要求传入的类型必须是具体类型，不能是 interface{}
2. 在高并发场景下，建议使用默认配置以获得最佳性能
3. [UnmarshalWithValidation](file:///D:/go/src/sun/sunshine/pkg/utils/jsonutil/jsonutil.go#L124-L137) 会拒绝包含结构体中未定义字段的 JSON 数据
4. 全局配置的修改会影响后续所有使用默认 API 的操作