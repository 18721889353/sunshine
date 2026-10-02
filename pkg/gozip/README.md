# gozip

ZIP 压缩工具包：把文件列表或整个目录打成 ZIP，支持 ZipCrypto/AES 加密与中文文件名，并把压缩过程接入 OTel 链路追踪。

## 架构概览

```
pkg/gozip/
├── zip.go                 # 包入口：类型与常量、文件列表压缩、writeZipEntries 压缩核心
├── dir.go                 # 目录递归压缩：遍历、相对路径条目收集（collectDirEntries）
├── zip_test.go            # zip.go 对应单元测试
├── dir_test.go            # dir.go 对应单元测试
├── test_helpers_test.go   # 跨测试文件共享的 helper（createTestFile/openZip 等）
├── benchmark_test.go      # 性能回归基线（本地磁盘，无网络 RTT）
├── fuzz_test.go           # 压缩-解压往返不变量
├── README.md              # 本文档
└── CHANGELOG.md           # 变更记录
```

**核心设计原则**：

- **单一压缩核心**：文件列表压缩与目录压缩共用 `writeZipEntries`，两者唯一差异是条目名的计算方式（`zipEntry{srcPath, entryName}`），避免两份写入逻辑漂移。
- **流式写入不占内存**：`os.Open + io.Copy` 直接把源文件泵进 ZIP 写入器，大文件不会整块驻留内存。
- **失败不留残骸**：目标文件创建后任一环节失败，defer 统一兜底关闭资源并删除半成品 ZIP。
- **编码兼容优先**：条目名统一正斜杠（ZIP 规范），并强制置 UTF-8 标志位（`0x800`），中文/亚洲字符文件名在 Windows 解压不乱码。
- **链路追踪内建**：每个导出 API 创建 OTel Span，从 `ctx` 提取 `request_id` 写入 `gozip.request_id` 属性，日志全部走 `logger.XxxWithCtx`。

## 职责边界

| 能力 | 是否本包职责 | 归属 |
|------|--------------|------|
| ZIP 压缩（文件列表 / 目录递归 / 加密） | 是 | 本包 |
| ZIP 解压（读取、提取） | 否 | 本包不提供解压 API；验证产物可用 `github.com/yeka/zip` 直接读 |
| 文件上传 / 分发 | 否 | `pkg/goupload` |
| tar / gz 等其他归档格式 | 否 | 本包只处理 ZIP |

复核命令：`grep -rn "func Unzip\|func Extract" pkg/gozip/`（应无结果）

## 使用场景选择

### 场景一：压缩指定文件列表（`ZipFilesFromPaths`）

**适用场景**：若干分散文件需要打成一个带密码的 ZIP（如导出报表）。

```go
import (
    "context"
    "log"

    "github.com/18721889353/sunshine/pkg/gozip"
)

files := []string{
    "/data/file1.xlsx",
    "/data/file2.xlsx",
}
// destPath 传 "" 时自动创建唯一临时文件，路径从 result.Path 取
result, err := gozip.ZipFilesFromPaths(context.Background(), files, "", "password123")
if err != nil {
    log.Printf("压缩失败: %v", err)
    return
}
log.Printf("压缩完成: %s, 大小 %d bytes, 文件数 %d, 加密 %v",
    result.Path, result.Size, result.FileCount, result.IsEncrypted)
```

**内部行为**：

1. 归一化并校验选项（`nil` 走默认值，返回副本不改调用方结构体；未知加密类型 / 非法压缩级别直接报错）。
2. 条目名取每个源文件的 `filepath.Base`（与历史行为一致）。
3. 逐个 `os.Open` → `CreateHeader`（置 UTF-8 标志，按需设密码/加密算法）→ `io.Copy` 流式写入。
4. 显式 `zipWriter.Close()` 写 central directory 并检查错误，再关闭文件、`Stat` 取大小。
5. 任一步失败：记录 Span 错误、删除已创建的目标文件、返回包装后的中文错误。

**注意**：同一批文件中若存在同名文件，条目名会相同（ZIP 允许重复条目但解压时互相覆盖）；需要保留路径结构请用场景二。

---

### 场景二：压缩整个目录（`ZipDirectory`）

**适用场景**：目录树需要完整归档，子目录结构必须保留。

```go
result, err := gozip.ZipDirectory(context.Background(), "/data/project", "/out/project.zip", "password123")
if err != nil {
    log.Printf("压缩目录失败: %v", err)
    return
}
```

**内部行为**：

1. 校验目录存在且非空。
2. `filepath.WalkDir` 收集文件，条目名 = 相对 `sourceDir` 的路径（`filepath.ToSlash` 转正斜杠）。
3. 交给 `writeZipEntries` 写入（与场景一同一条核心路径）。

**注意**：不同子目录下的同名文件（如 `data.txt` 与 `nested/data.txt`）会作为两个独立条目保留，不会互相覆盖。

---

### 场景三：自定义选项（`ZipFilesFromPathsWithOptions` / `ZipDirectoryWithOptions`）

**适用场景**：需要控制加密算法、压缩方法或目录条目前缀。

```go
options := &gozip.ZipOptions{
    Password:       "mypassword",
    Encryption:     gozip.ZipAES256Encryption, // AES-256（推荐）
    Compression:    gozip.ZipNoCompression,    // 仅存储不压缩（已是压缩格式的文件适用）
    IncludeBaseDir: true,                      // 目录压缩时条目带上目录名前缀
}
result, err := gozip.ZipDirectoryWithOptions(context.Background(), "/data/project", "/out/project.zip", options)
if err != nil {
    log.Printf("压缩失败: %v", err)
    return
}
```

**内部行为**：与对应的基础版本一致，仅条目命名（`IncludeBaseDir`）与压缩方法（`Compression`）不同。

**注意**：

- `IncludeBaseDir` 只对目录压缩生效，文件列表压缩忽略该字段。
- **两套 API 加密强度一致**：`ZipOptions.Encryption` 零值（`ZipDefaultEncryption`）归一为 AES-256，
  与便捷函数默认一致——从 `ZipFilesFromPaths` 迁移到 `WithOptions` 只加 `Compression` 不会降低加密强度；
  需要 ZipCrypto 请显式传 `Encryption: gozip.ZipStandardEncryption`。
- **非法选项 fail loud**：未知加密类型或 `ZipNoCompression`/`ZipDefaultCompression`/`ZipBestSpeed`/
  `ZipBestCompression` 之外的压缩级别会返回错误，且不创建目标文件（校验发生在任何磁盘写入之前）。

## 参数/结构体说明

### ZipOptions

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `Password` | `string` | 否 | 压缩密码；为空则不加密，`Encryption`/`IsEncrypted` 均以它为准 |
| `Encryption` | `ZipEncryptionType` | 否 | 加密算法；**零值 `ZipDefaultEncryption` 等价 AES-256**（与便捷函数一致），需 ZipCrypto 请显式赋 `ZipStandardEncryption`；未知值返回错误 |
| `Compression` | `int` | 否 | 压缩方法；`ZipNoCompression` 写 `zip.Store`，其余写 `zip.Deflate`；零值即默认，其余非法值返回错误 |
| `IncludeBaseDir` | `bool` | 否 | 仅目录压缩生效：条目名是否以源目录名开头 |
| `TempDir` | `string` | 否 | 自动生成临时文件的目录（仅 `destPath` 为空时生效）；空则用系统默认临时目录，目录须已存在 |

### ZipFileResult

| 字段 | 类型 | 说明 |
|------|------|------|
| `Path` | `string` | 实际生成的 ZIP 路径（`destPath` 为空时为自动创建的临时文件路径） |
| `Size` | `int64` | ZIP 文件大小（字节）；`Stat` 失败时降级为 0 并告警，不影响压缩结果 |
| `FileCount` | `int` | 写入的文件条目数 |
| `IsEncrypted` | `bool` | 是否加密（等价于 `Password != ""`） |

### 压缩级别常量

| 常量 | 值 | 实际效果 |
|------|----|----------|
| `ZipDefaultCompression` | 0 | `zip.Deflate` |
| `ZipBestSpeed` | -1 | `zip.Deflate`（与默认相同） |
| `ZipBestCompression` | -2 | `zip.Deflate`（与默认相同） |
| `ZipNoCompression` | -3 | `zip.Store`（不压缩，仅存储） |

**重要限制**：底层库 `yeka/zip` 未实现 deflate 级别调节（源码 `writer.go:17` 为 `TODO(adg): support specifying deflate level`），因此级别常量只决定「压不压」，**不改变压缩强度**；只有 `ZipNoCompression` 与其他常量有可观测差异。

### 加密类型常量

| 常量 | 值 | 说明 |
|------|----|------|
| `ZipDefaultEncryption` | 0 | 零值语义：未设置时的默认，在归一化后等价 AES-256 |
| `ZipStandardEncryption` | 1 | 标准 ZipCrypto：兼容性最好，安全性较低 |
| `ZipAES128Encryption` | 2 | AES-128 |
| `ZipAES192Encryption` | 3 | AES-192 |
| `ZipAES256Encryption` | 4 | AES-256（推荐，最安全） |

**加密默认值一致性**：`ZipFilesFromPaths` / `ZipDirectory` 便捷函数在密码非空时直接使用 AES-256；
`WithOptions` 版本的零值同样归一为 AES-256——两套 API 对同一调用产生同等强度的产物（由
`TestZipZeroValueEncryptionMatchesConvenience` 守护，以 WinZip AES extra field `0x9901` 为判别依据）。

**安全说明**：

- `Password` 为 Go `string`，语言层面无法在使用后擦除内存；密码从内存清理由调用方负责。
- AES 条目会写入 WinZip AES extra field（`0x9901`）；`yeka/zip` 读取时会把条目 `Method` 从 99 还原为
  原始压缩方法（`reader.go:313`），因此**不要用 `Method` 判断加密算法**。

## API 速查

### ZipFilesFromPaths — 压缩文件列表（带密码便捷入口）

```go
func ZipFilesFromPaths(ctx context.Context, sourceFiles []string, destPath string, password string) (*ZipFileResult, error)
```

- `password` 非空时使用 AES-256 加密
- `destPath` 为空时创建唯一临时文件（`os.CreateTemp`，并发安全）
- **注意**：`sourceFiles` 为空、文件不存在均返回错误，且不残留目标文件

### ZipFilesFromPathsWithOptions — 压缩文件列表（完整选项）

```go
func ZipFilesFromPathsWithOptions(ctx context.Context, sourceFiles []string, destPath string, options *ZipOptions) (*ZipFileResult, error)
```

- `options` 可为 `nil`（走默认值）；传入的结构体不会被修改
- **注意**：条目名取 `filepath.Base`，同名文件会互相覆盖

### ZipDirectory — 递归压缩目录（带密码便捷入口）

```go
func ZipDirectory(ctx context.Context, sourceDir string, destPath string, password string) (*ZipFileResult, error)
```

- 条目保留相对路径结构，AES-256 加密
- **注意**：空目录返回错误

### ZipDirectoryWithOptions — 递归压缩目录（完整选项）

```go
func ZipDirectoryWithOptions(ctx context.Context, sourceDir string, destPath string, options *ZipOptions) (*ZipFileResult, error)
```

- `IncludeBaseDir=true` 时条目名为 `目录名/相对路径`
- **注意**：`sourceDir` 必须是已存在的目录，否则返回错误

## 错误处理

| 场景 | 行为 |
|------|------|
| `sourceFiles` 为空 | 返回 `源文件列表为空`，不创建任何文件 |
| 未知加密类型 / 非法压缩级别 | 返回 `未知的加密类型: <值>` / `未知的压缩级别: <值>`，不创建任何文件（校验先于落盘） |
| `TempDir` 不存在 | 返回 `创建临时ZIP文件失败: %w`（`os.CreateTemp` 失败，不产生任何文件） |
| 源文件不存在 / 无权限 | 返回 `写入ZIP条目 [名] 失败: ...`（`%w` 包装底层错误），已创建的目标文件被删除 |
| `sourceDir` 为空 / 不存在 / 不是目录 | 分别返回对应中文错误 |
| 目录为空 | 返回 `目录为空: <path>` |
| `destPath` 创建失败 | 返回 `创建ZIP文件失败: %w` / `创建临时ZIP文件失败: %w` |
| 写 central directory 失败 | 返回 `关闭ZIP写入器失败: %w`，半成品被删除 |
| 结果大小 `Stat` 失败 | **不报错**：size 记 0 并输出 `获取压缩文件大小失败` 告警日志 |
| 关闭文件/写入器失败（defer 兜底路径） | 仅记录 `关闭ZIP写入器失败` / `关闭ZIP文件失败` 告警日志 |

所有错误均用 `%w` 包装，可用 `errors.Is` / `errors.As` 判定底层错误；错误消息与日志均为中文。

## 测试

本包**不提供 `integration_test.go`**：压缩只依赖本地文件系统，没有真实外部服务，按质量基线无需集成测试分层。

```bash
# 单元测试 + fuzz 种子语料（日常）
go test ./pkg/gozip/ -count=1

# 性能基线（见下节）
go test ./pkg/gozip/ -run '^$' -bench . -benchmem -benchtime 100x

# 模糊测试挖掘（种子语料已随常规 go test 执行）
go test ./pkg/gozip/ -fuzz FuzzZipRoundTrip -fuzztime 30s
```

测试文件与源文件一对一：`zip_test.go` ↔ `zip.go`、`dir_test.go` ↔ `dir.go`，共享 helper 集中在 `test_helpers_test.go`。

## 性能基线与模糊测试

### 竞态检测

```bash
CGO_ENABLED=1 go test -race -count=1 -short ./pkg/gozip/
```

> **诚实声明**：本机（Windows 22H2 + MinGW）执行上述命令报 `exit status 0xc0000139`（动态链接器加载失败），因此 **本 README 不声称已经跑过 `-race`**；真实证据需在 Linux/CI 环境采集。

### 基准测试

实测读数（Windows 22H2，`11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz`，`go test -bench . -benchmem -benchtime 100x -count 3`，3 轮）：

| 基准 | 场景 | ns/op | B/op | allocs/op | benchtime |
|------|------|-------|------|-----------|-----------|
| `BenchmarkZipFilesFromPathsNoPassword` | 10 个小文件，无密码 Deflate | 816,434 ~ 967,741 | ~360,000 | 222 ~ 223 | 100x |
| `BenchmarkZipFilesFromPathsWithAES256` | 10 个小文件，AES-256 加密 | 8,333,975 ~ 8,554,650 | ~386,820 ~ 403,163 | 522 ~ 523 | 100x |
| `BenchmarkZipDirectory` | 3 文件 1 子目录，无密码 | 753,629 ~ 768,653 | ~116,683 ~ 125,512 | 133 | 100x |

**口径与解读（很重要，否则数字会被误读）：**

- 全部在本地磁盘临时目录执行，**不含网络 RTT**；读数量的是本包自身的压缩与文件 I/O 开销。
- `ns/op` 波动较大（首轮含文件系统缓存冷启），跨轮比较请优先看稳定的 `allocs/op`。
- AES-256 比无密码慢约 10 倍，来自每条目密钥派生与 HMAC，属加密固有成本；`allocs/op` 的差值（约 300）即加密路径的分配开销。
- 每条结论对应上面一次真实重测（3 轮读数已给出区间），**不是**多机器统计。

### 模糊测试

| Target | 不变量 |
|--------|--------|
| `FuzzZipRoundTrip` | 任意（文件名、内容、密码）组合压缩后：条目名逐字节等于源文件名、UTF-8 标志位 `0x800` 始终置位、解压内容与原文逐字节一致，且全程不 panic |

- 非法文件名（路径分隔符、Windows 保留字符、设备名等）走 `t.Skip`——那是环境限制而非被测代码缺陷
- 种子语料（中文名、韩文名、空内容、带空格名、带密码）随常规 `go test` 执行；挖掘需显式 `-fuzz`
