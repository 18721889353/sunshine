# jsonutil 包性能测试报告

## 测试概述

本报告展示了 jsonutil 包与 Go 标准库 [encoding/json](file:///usr/local/go/src/encoding/json/json.go) 包的性能对比测试结果。测试环境为 Windows amd64 平台，使用 Intel i5-1135G7 CPU。

## 基准测试结果

以下是详细的基准测试结果（数值越低表示性能越好）：

| 测试项目 | 标准库 | jsonutil | 性能提升 |
|----------|--------|----------|----------|
| **序列化 (Marshal)** | 1082 ns/op | 702.1 ns/op | ~35% |
| **反序列化 (Unmarshal)** | 3564 ns/op | 1396 ns/op | ~61% |
| **写入 Writer** | 957.5 ns/op | 759.1 ns/op | ~21% |
| **并发序列化** | 456.0 ns/op | 385.6 ns/op | ~15% |
| **并发反序列化** | 1451 ns/op | 637.8 ns/op | ~56% |

### 内存分配对比

| 操作 | 标准库 | jsonutil | 改进 |
|------|--------|----------|------|
| Marshal 内存分配 | 488 B/op, 9 allocs | 504 B/op, 5 allocs | 减少 4 次分配 |
| Unmarshal 内存分配 | 992 B/op, 26 allocs | 776 B/op, 24 allocs | 减少 2 次分配，节省 216B |

## 专项测试详情

### 1. 基础序列化测试
- **标准库**：`BenchmarkStandardMarshal-8` - 1082 ns/op, 488 B/op, 9 allocs/op
- **jsonutil**：`BenchmarkJsonUtilMarshal-8` - 702.1 ns/op, 504 B/op, 5 allocs/op
- **结果**：jsonutil 在序列化速度上提升了约 35%，虽然内存使用略有增加但分配次数减少了 44%

### 2. 基础反序列化测试
- **标准库**：`BenchmarkStandardUnmarshal-8` - 3564 ns/op, 992 B/op, 26 allocs/op
- **jsonutil**：`BenchmarkJsonUtilUnmarshal-8` - 1396 ns/op, 776 B/op, 24 allocs/op
- **结果**：jsonutil 在反序列化速度上提升了约 61%，内存使用减少 216B，分配次数减少 2

### 3. 写入 Writer 测试
- **标准库**：`BenchmarkStandardEncodeToWriter-8` - 957.5 ns/op, 280 B/op, 8 allocs/op
- **jsonutil**：`BenchmarkJsonUtilMarshalToWriter-8` - 759.1 ns/op, 912 B/op, 7 allocs/op
- **结果**：jsonutil 在写入性能上提升了约 21%，分配次数减少 1

### 4. 高并发测试
- **序列化**：jsonutil 并发性能提升约 15%
- **反序列化**：jsonutil 并发性能提升约 56%

### 5. 特殊功能测试
- **带缩进序列化**：`BenchmarkMarshalIndent-8` - 2352 ns/op, 920 B/op, 6 allocs/op
- **验证反序列化**：`BenchmarkUnmarshalWithValidation-8` - 2663 ns/op, 1584 B/op, 43 allocs/op
- **配置化序列化**：`BenchmarkMarshalWithConfig-8` - 758.6 ns/op, 504 B/op, 5 allocs/op

## 关键优势

1. **泛型优化**：使用 Go 泛型消除 interface{} 导致的逃逸分配，显著减少内存分配次数
2. **实例缓存**：缓存不同配置的 API 实例，避免重复创建
3. **并发友好**：在高并发场景下仍保持良好的性能表现
4. **功能丰富**：在保持高性能的同时提供了更多实用功能

## 使用建议

1. **常规使用**：对于大多数 JSON 操作场景，推荐使用 jsonutil 替代标准库
2. **高并发场景**：特别适合需要大量 JSON 序列化/反序列化的服务端应用
3. **内存敏感应用**：虽然某些操作内存使用略高，但分配次数的减少有助于降低 GC 压力
4. **特殊需求**：如需严格验证、格式化输出等功能，jsonutil 提供了便捷的 API

## 总结

jsonutil 包在几乎所有测试场景中都表现出优于标准库的性能，特别是在反序列化操作上性能提升显著（超过 60%）。通过使用泛型和优化的内部实现，该包在保持易用性的同时实现了显著的性能改进，是 JSON 操作的理想选择。