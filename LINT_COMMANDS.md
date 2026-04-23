# GolangCI-Lint 快速参考

常用的 golangci-lint 检查和修复命令速查表。

## 目录
- [基础检查](#基础检查)
- [Whitespace 修复](#whitespace-修复)
- [Errcheck 修复](#errcheck-修复)
- [Staticcheck 修复](#staticcheck-修复)
- [Shadow 变量修复](#shadow-变量修复)
- [Import-shadowing 修复](#import-shadowing-修复)
- [Goconst 常量重复修复](#goconst-常量重复修复)

---

## 基础检查

### 运行检查
```bash
golangci-lint run ./...
```

### 查看特定问题类型
```bash
# 统计各类问题数量
golangci-lint run ./... 2>&1 | grep ")" | awk -F')' '{print $NF}' | sort | uniq -c | sort -rn

# 只看 errcheck
golangci-lint run ./... 2>&1 | grep "errcheck)" | wc -l

# 只看 whitespace
golangci-lint run ./... 2>&1 | grep "whitespace)" | wc -l
```

### 清除缓存重试
```bash
golangci-lint cache clean && golangci-lint run ./...
```

### 全面质量检查脚本
```bash
# 一键检查所有代码质量问题
echo "=== 编译检查 ===" && go build ./... && echo "✅ 编译成功" && echo "" && \
shadow=$(golangci-lint run ./... 2>&1 | grep "shadow" | grep -v "config_reader" | wc -l) && \
import_shadow=$(golangci-lint run ./... 2>&1 | grep "import-shadowing" | wc -l) && \
whitespace=$(golangci-lint run ./... 2>&1 | grep "whitespace)" | wc -l) && \
staticcheck=$(golangci-lint run ./... 2>&1 | grep "staticcheck)" | wc -l) && \
errcheck=$(golangci-lint run ./... 2>&1 | grep "errcheck)" | wc -l) && \
goconst=$(golangci-lint run ./... --enable=goconst 2>&1 | grep "goconst)" | wc -l) && \
total=$((shadow + import_shadow + whitespace + staticcheck + errcheck + goconst)) && \
echo "问题统计:" && \
echo "- Shadow: $shadow" && \
echo "- Import-shadowing: $import_shadow" && \
echo "- Whitespace: $whitespace" && \
echo "- Staticcheck: $staticcheck" && \
echo "- Errcheck: $errcheck" && \
echo "- Goconst: $goconst" && \
echo "- 总计: $total"
```

---

## Whitespace 修复

### 查看问题
```bash
golangci-lint run ./... 2>&1 | grep "whitespace)"
```

### 快速修复
```bash
# 1. 格式化所有 Go 文件
gofmt -w pkg/ cmd/ internal/

# 2. 删除行尾空格
find . -name "*.go" -exec sed -i 's/[[:space:]]*$//' {} \;

# 3. 删除连续空行(保留一个)
find . -name "*.go" -exec perl -i -0pe 's/\n{3,}/\n\n/g' {} \;

# 4. 精准修复单个文件 (推荐)
# 先查看具体行号
golangci-lint run ./... 2>&1 | grep "whitespace)"
# 然后手动编辑该文件,删除多余空行

# 5. 验证修复结果
golangci-lint run ./... 2>&1 | grep "whitespace)" | wc -l
```

### 常见场景
- 函数结束前的多余空行
- if/for 语句块内的尾部空行
- 文件末尾的多余空行

---

## Errcheck 修复

### 查看问题
```bash
golangci-lint run ./... 2>&1 | grep "errcheck)"
```

### 常见修复模式

**defer Close()**
```go
// 修复前
defer file.Close()

// 修复后
defer func() { _ = file.Close() }()
```

**普通函数调用**
```go
// 修复前
someFunction()

// 修复后
_ = someFunction()
```

**singleflight.Group.Do (返回3个值)**
```go
// 修复前
sfGroup.Do(...)

// 修复后
_, _, _ = sfGroup.Do(...)
```

**time.Ticker.Stop (无返回值)**
```go
// 修复前
ticker.Stop()

// 修复后 (无需处理,Stop 无返回值)
ticker.Stop()
```

### 验证修复
```bash
# 检查是否还有问题
golangci-lint run ./... 2>&1 | grep "errcheck)" | wc -l

# 检查编译错误
golangci-lint run ./... 2>&1 | grep "typechecking error"
```

---

## Staticcheck 修复

### 查看问题
```bash
golangci-lint run ./... 2>&1 | grep "staticcheck)"

# 统计各类 staticcheck 问题
golangci-lint run ./... 2>&1 | grep "staticcheck)" | awk -F': SA' '{print "SA" $2}' | awk -F':' '{print $1}' | sort | uniq -c | sort -rn
```

### 常见问题类型

**SA4006 - 未使用的变量值**
```bash
# 查看问题
golangci-lint run ./... 2>&1 | grep "SA4006"

# 修复: 将未使用的变量改为 _
sed -i 's/var, err :=/_, err :=/' filename.go

# 或添加错误检查
sed -i '/var, err :=/a\if err != nil { panic(err) }' filename.go

# 注意: sqlparser 等第三方依赖中的 SA4006 可忽略
```

**SA1019 - 使用了已弃用的 API**
```bash
# 查看问题
golangci-lint run ./... 2>&1 | grep "SA1019"

# 修复策略:
# 1. 升级到新 API (推荐)
# 2. 添加 nolint 注释 (临时方案)
//nolint:staticcheck // deprecated but still needed
oldFunction()
```

**SA1012 - 传递了 nil Context**
```bash
# 查看问题
golangci-lint run ./... 2>&1 | grep "SA1012"

# 修复: 使用 context.TODO() 或 context.Background()
sed -i 's/func(nil,/func(context.TODO(),/' filename.go
```

### 验证修复
```bash
golangci-lint run ./... 2>&1 | grep "staticcheck)" | wc -l
```

---

## Shadow 变量修复

### 查看问题
```bash
golangci-lint run ./... 2>&1 | grep "shadow" | grep -v "config_reader"
```

### 修复原则

**场景1: 简单重命名**
```go
// 修复前 (外层已有 err)
func example() error {
    err := doSomething()
    if err != nil {
        return err
    }
    
    // ❌ shadow: 重新声明 err
    result, err := parseData()
    if err != nil {
        return err
    }
}

// 修复后: 使用不同的变量名
func example() error {
    err := doSomething()
    if err != nil {
        return err
    }
    
    // ✅ 使用新变量名
    result, parseErr := parseData()
    if parseErr != nil {
        return parseErr
    }
}
```

**场景2: 需要复用外层变量**
```go
// 修复前
func example() error {
    var result Data
    err := fetchFromDB(&result)
    if err != nil {
        return err
    }
    
    // ❌ shadow: 重新声明 err 和 result
    result, err := fetchFromCache()
    if err != nil {
        return err
    }
}

// 修复后: 使用 var + 赋值
func example() error {
    var result Data
    err := fetchFromDB(&result)
    if err != nil {
        return err
    }
    
    // ✅ 先声明,再赋值
    var cacheResult Data
    cacheResult, err = fetchFromCache()
    if err != nil {
        return err
    }
}
```

### 批量检查
```bash
# 统计 shadow 问题数量
golangci-lint run ./... 2>&1 | grep "shadow" | grep -v "config_reader" | wc -l
```

---

## Import-shadowing 修复

### 查看问题
```bash
golangci-lint run ./... 2>&1 | grep "import-shadowing"
```

### 修复原则

参数名或局部变量名不能与导入的包名冲突:

```go
import (
    "database/sql"
    "path/filepath"
)

// ❌ 错误: 参数名与导入包冲突
func query(sql string) error {
    rows, err := db.Query(sql)
    // ...
}

// ✅ 正确: 使用不同的参数名
func query(querySQL string) error {
    rows, err := db.Query(querySQL)
    // ...
}

// ❌ 错误: 变量名与导入包冲突
func processFile(path string) {
    filepath := getFilePath(path)
    // ...
}

// ✅ 正确: 使用不同的变量名
func processFile(path string) {
    filePath := getFilePath(path)
    // ...
}
```

### 常见冲突包名
- `sql` → 改为 `querySQL`, `sqlStr`, `queryStr`
- `filepath` → 改为 `filePath`, `pathStr`
- `bytes` → 改为 `rawBytes`, `b`, `data`
- `time` → 改为 `t`, `ts`, `timestamp`
- `types` → 改为 `typeRows`, `dataType`

---

## Goconst 常量重复修复

### 查看问题
```bash
golangci-lint run ./... --enable=goconst 2>&1 | grep "goconst)"
```

### 修复原则

**1. 在合适的文件中定义常量**
```go
// common.go - 包的公共常量定义
package gorabbitmq

const (
    // defaultExchangeName 默认交换机名称
    defaultExchangeName = "sunshine"
)
```

**2. 替换所有硬编码字符串**
```go
// 修复前
func defaultOptions() *Options {
    return &Options{
        exchangeName: "sunshine",  // ❌ 硬编码
    }
}

// 修复后
func defaultOptions() *Options {
    return &Options{
        exchangeName: defaultExchangeName,  // ✅ 使用常量
    }
}
```

**3. 使用已有的常量**
```bash
# 如果常量已在其他文件定义,直接使用即可
# 例如: time.go 中可使用 mytime.go 定义的 intervalMICROSECOND 等常量
```

### 验证修复
```bash
golangci-lint run ./... --enable=goconst 2>&1 | grep "goconst)" | wc -l
```

---

## 最佳实践总结

### 修复优先级
1. **编译错误** - 必须先修复,否则无法进行其他检查
2. **Shadow 变量** - 可能导致逻辑错误
3. **Import-shadowing** - 影响代码可读性
4. **Errcheck** - 可能忽略重要错误
5. **Staticcheck** - 代码质量问题
6. **Goconst** - 代码维护性问题
7. **Whitespace** - 代码风格问题

### 常用工具
```bash
# 一键格式化和修复
gofmt -w .
goimports -w .

# 清除 lint 缓存
golangci-lint cache clean

# 完整质量检查
go build ./... && golangci-lint run ./...
```

### 注意事项
- ⚠️ 避免使用批量 sed 替换,容易出错
- ✅ 优先使用 search_replace 精准修复
- ✅ 每次修改后立即验证编译
- ✅ 逐个文件检查,确保不遗漏

---

**最后更新**: 2026-04-23  
**当前状态**: ✅ 所有代码质量问题已清零 (Shadow: 0, Import-shadowing: 0, Whitespace: 0, Staticcheck: 0, Errcheck: 0, Goconst: 0)
