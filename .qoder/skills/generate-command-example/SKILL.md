---
name: generate-command-example
description: 规范 `cmd/sunshine/commands/generate/` 下所有命令的文档格式，包括 Use/Short/Long/Example/flag usage 全部使用中文并遵循统一风格。
---

# generate 命令文档格式规范

## 概述

所有 `cmd/sunshine/commands/generate/` 下的命令，其 `Use`、`Short`、`Long`、`Example`、flag usage 描述**全部使用中文**，并遵循统一格式。
以 `dao.go` 为**标准参考**。

## 规则清单

### 一、Use / Short / Long — 中文描述

| 字段 | 说明 | 示例 |
|------|------|------|
| `Use` | 子命令名称，保持英文短名 | `"dao"` |
| `Short` | 一行简短中文描述 | `"基于 SQL 自动生成 DAO 层代码"` |
| `Long` | 详细中文描述，末尾加句号 | `"基于 SQL 表结构自动生成数据访问层（DAO）代码，包含 CRUD 操作方法。"` |

### 二、Example — 标准化格式

```go
Example: color.HiBlackString(fmt.Sprintf(`  # =====================================================================
  # 基本用法：<一句话描述命令功能>
  # 执行后会生成: <列出生成的文件路径>
  # =====================================================================
  sunshine %[1]s <子命令> \
    --flag1=value1 \
    --flag2=value2 \
    --flag3=value3


  # =====================================================================
  # 参数说明：
  #   --flag-name     参数描述（必填/可选，默认值）
  #   --flag-name2    参数描述（必填/可选，默认值）
`, parentName)),
```

| # | 规则 | 说明 |
|---|------|------|
| 1 | **单个 `%[1]s`** | 使用 `%[1]s` 引用 `parentName`，禁止用多个 `%s`。无 parentName 的命令直接写 `sunshine` |
| 2 | **`# =====` 分隔线** | 每个段落用 `# =====================================================================` 分隔 |
| 3 | **基本用法段** | 开头段落：分隔线 + 用途描述 + 生成文件说明 + 分隔线 |
| 4 | **命令分行** | 使用 `\` 分行，每个 flag 一行，缩进 4 空格 |
| 5 | **空行分隔** | 命令示例与参数说明之间留 2 个空行 |
| 6 | **参数说明段** | `# 参数说明：` + 每个 flag 一行，格式：`#   --flag  描述（必填/可选，默认值）` |
| 7 | **参数对齐** | flag 名左对齐，描述文字统一缩进到同一列（至少 2 空格间隔） |
| 8 | **flag 完整列出** | 列出该命令**所有** flag，包括可选的 |

### 三、Flag Usage — 中文描述

所有 `cmd.Flags().XxxVarP()` 的 usage 描述参数必须使用中文，格式为：

```
"<参数描述>（必填/可选，默认 <值>）"
```

不标注"必填"表示由 `MarkFlagRequired` 标记，usage 中只需写参数含义。

| 写法 | 说明 | 示例 |
|------|------|------|
| 必填 | 由 `MarkFlagRequired` 标记，usage 只写含义 | `"Go 模块名，对应 go.mod 文件中的 module 声明"` |
| 选填+默认值 | 标注可选和默认值 | `"JSON 标签风格，0:下划线, 1:驼峰（可选，默认 1）"` |
| 选填+默认路径 | 标注可选和默认目录 | `"输出目录（可选，默认 ./dao_<时间戳>）"` |

## 已完成（全部 16 个命令）

| 命令 | 文件 | Example | Short/Long | Flag usage |
|------|------|---------|------------|------------|
| `dao` | `dao.go` | ✅ | ✅ | ✅ |
| `model` | `model.go` | ✅ | ✅ | ✅ |
| `grpc-http-pb` | `grpc-http-pb.go` | ✅ | ✅ | ✅ |
| `cache` | `cache.go` | ✅ | ✅ | ✅ |
| `handler` | `handler.go` | ✅ | ✅ | ✅ |
| `handler-pb` | `handler-pb.go` | ✅ | ✅ | ✅ |
| `http` | `http.go` | ✅ | ✅ | ✅ |
| `http-pb` | `http-pb.go` | ✅ | ✅ | ✅ |
| `protobuf` | `protobuf.go` | ✅ | ✅ | ✅ |
| `service` | `service.go` | ✅ | ✅ | ✅ |
| `service-handler` | `service-handler.go` | ✅ | ✅ | ✅ |
| `rpc` | `rpc.go` | ✅ | ✅ | ✅ |
| `rpc-pb` | `rpc-pb.go` | ✅ | ✅ | ✅ |
| `rpc-gw-pb` | `rpc-gw-pb.go` | ✅ | ✅ | ✅ |
| `rpc-conn` | `rpc-conn.go` | ✅ | ✅ | ✅ |
| `swagger` | `swag-json.go` | ✅ | ✅ | ✅ |

所有命令的 Use/Short/Long/Example/flag usage 已全部统一为中文标准化格式。

## 示例对照

### ❌ 旧格式（英文 + 零散）

```go
Short: "Generate grpc+http service code based on protobuf file",
Example: color.HiBlackString(`  # Generate grpc+http service code.
  sunshine micro grpc-http-pb --module-name=... --server-name=...
`),
// flag usage
cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "module-name is the name of the module in the go.mod file")
```

### ✅ 新格式（中文 + 标准化）

```go
Short: "根据 protobuf 文件生成 gRPC+HTTP 混合服务代码",
Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：根据 protobuf 文件生成 gRPC+HTTP 混合服务代码
  # =====================================================================
  sunshine micro grpc-http-pb \
    --module-name=yourModuleName \
    --server-name=yourServerName
  # =====================================================================
  # 参数说明：
  #   --module-name     Go 模块名（必填）
  #   --server-name     服务名（必填）
`, parentName)),
// flag usage
cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "Go 模块名，对应 go.mod 文件中的 module 声明")
```

## 实现步骤

1. 定位命令文件（如 `protobuf.go`）
2. 将 `Short`/`Long` 改为中文描述
3. 按标准格式重写整个 Example 字符串
4. 将 `%s` 改为 `%[1]s`，删除多余的参数
5. 添加 `# =====` 分隔线和参数说明段
6. 将所有 flag usage 描述改为中文
7. 运行 `go vet ./cmd/sunshine/commands/generate/` 验证无误
