# GoExcel - Excel 文件处理工具

## 📦 概述

`github.com/18721889353/sunshine/pkg/goexcel` 提供了简单易用的 Excel 文件导出功能，基于 [excelize](https://xuri.me/excelize/) 库构建。

### 功能特性

- ✅ 支持导出 Excel 文件（带表头和数据）
- ✅ 自动处理列名映射（A-Z, AA-AZ, AAA等）
- ✅ **支持多工作表导出**
- ✅ **支持流式写入（高性能，适合大数据量）**
- ✅ **支持分批导出（避免内存溢出）**
- ✅ 支持混合数据类型（字符串、数字、布尔值等）
- ✅ 支持特殊字符（中文、emoji、换行符等）
- ✅ 高性能内存优化
- ✅ 完整的错误处理
- ✅ **无多余空Sheet（自动处理默认Sheet1）**

---

## 🚀 快速开始

### 基础用法

```go
package main

import (
    "context"
    "log"
    "github.com/18721889353/sunshine/pkg/goexcel"
)

func main() {
    // 定义表头
    headers := []string{"姓名", "年龄", "城市"}
    
    // 定义数据行
    rows := [][]interface{}{
        {"张三", 25, "北京"},
        {"李四", 30, "上海"},
        {"王五", 28, "广州"},
    }
    
    // 导出 Excel
    f, err := goexcel.ExportExcel(context.Background(), "用户列表", headers, rows)
    if err != nil {
        log.Fatal(err)
    }
    defer f.Close()
    
    // 保存文件
    if err := f.SaveAs("users.xlsx"); err != nil {
        log.Fatal(err)
    }
    
    log.Println("Excel 文件导出成功!")
}
```

---

## 📊 场景选择指南

| 数据量 | 推荐方案 | 内存占用 | 耗时 | API |
|--------|---------|---------|------|-----|
| < 1万行 | 单Sheet直接导出 | < 50MB | < 100ms | `ExportExcel` |
| 1万 - 10万行 | 单Sheet直接导出 | 50-200MB | 100ms-1s | `ExportExcel` |
| 10万 - 100万行 | **流式写入** ⭐ | < 200MB | 1-15s | `ExportLargeDataset` |
| > 100万行 | **分割多Sheet** | 按需 | 按需 | `SplitIntoSheets` + `ExportMultiSheetExcel` |

---

## 💡 使用示例

### 1. 基础导出 - 员工列表示例

**适用场景**: 简单的数据导出，行数较少（< 10万行）

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/18721889353/sunshine/pkg/goexcel"
)

func main() {
    // 定义表头
    headers := []string{"工号", "姓名", "部门", "职位", "入职日期"}
    
    // 模拟员工数据
    rows := [][]interface{}{
        {"EMP001", "张三", "技术部", "高级工程师", "2020-03-15"},
        {"EMP002", "李四", "产品部", "产品经理", "2019-07-20"},
        {"EMP003", "王五", "市场部", "市场总监", "2021-01-10"},
        {"EMP004", "赵六", "人事部", "HR经理", "2018-11-05"},
    }
    
    // 导出 Excel
    f, err := goexcel.ExportExcel(context.Background(), "员工列表", headers, rows)
    if err != nil {
        log.Fatalf("导出失败: %v", err)
    }
    defer f.Close()
    
    // 保存文件
    filename := fmt.Sprintf("employee_%d.xlsx", 20260521)
    if err := f.SaveAs(filename); err != nil {
        log.Fatalf("保存失败: %v", err)
    }
    
    log.Printf("成功导出 %d 条员工数据到 %s", len(rows), filename)
}
```

**输出结果:**
```
2026/05/21 10:30:00 成功导出 4 条员工数据到 employee_20260521.xlsx
```

---

### 2. 多工作表导出 - 综合报表

**适用场景**: 需要在同一个Excel文件中展示多个相关数据集

```go
package main

import (
    "context"
    "log"
    "github.com/18721889353/sunshine/pkg/goexcel"
)

func main() {
    // 定义多个工作表数据
    sheets := []goexcel.SheetData{
        {
            SheetName: "销售汇总",
            Headers:   []string{"月份", "销售额", "订单数", "客户数"},
            Rows: [][]interface{}{
                {"2024-01", 150000.50, 1200, 800},
                {"2024-02", 180000.75, 1500, 950},
                {"2024-03", 210000.30, 1800, 1100},
            },
        },
        {
            SheetName: "热销产品",
            Headers:   []string{"产品名称", "销量", "单价", "总收入"},
            Rows: [][]interface{}{
                {"产品A", 5000, 99.9, 499500.0},
                {"产品B", 3500, 149.9, 524650.0},
                {"产品C", 2800, 199.9, 559720.0},
            },
        },
        {
            SheetName: "地区分布",
            Headers:   []string{"地区", "销售额", "占比", "增长率"},
            Rows: [][]interface{}{
                {"华东", 250000.0, "45%", "12%"},
                {"华南", 180000.0, "32%", "8%"},
                {"华北", 130000.0, "23%", "15%"},
            },
        },
    }

    // 导出多工作表 Excel
    f, err := goexcel.ExportMultiSheetExcel(context.Background(), sheets)
    if err != nil {
        log.Fatalf("导出失败: %v", err)
    }
    defer f.Close()

    if err := f.SaveAs("sales_report.xlsx"); err != nil {
        log.Fatalf("保存失败: %v", err)
    }

    log.Printf("成功导出 %d 个工作表", len(sheets))
}
```

**输出结果:**
```
2026/05/21 10:35:00 成功导出 3 个工作表
```

**生成的Excel结构:**
- 📊 Sheet1: 销售汇总 (3行数据)
- 📊 Sheet2: 热销产品 (3行数据)
- 📊 Sheet3: 地区分布 (3行数据)

---

```go
package main

import (
    "context"
    "log"
    "github.com/18721889353/sunshine/pkg/goexcel"
)

func main() {
    // 定义多个工作表
    sheets := []goexcel.SheetData{
        {
            SheetName: "用户列表",
            Headers:   []string{"姓名", "年龄", "城市"},
            Rows: [][]interface{}{
                {"张三", 25, "北京"},
                {"李四", 30, "上海"},
            },
        },
        {
            SheetName: "订单列表",
            Headers:   []string{"订单号", "金额", "日期"},
            Rows: [][]interface{}{
                {"ORD001", 100.5, "2024-01-01"},
                {"ORD002", 200.8, "2024-01-02"},
            },
        },
    }

    f, err := goexcel.ExportMultiSheetExcel(context.Background(), sheets)
    if err != nil {
        log.Fatal(err)
    }
    defer f.Close()

    if err := f.SaveAs("multi_sheet.xlsx"); err != nil {
        log.Fatal(err)
    }

    log.Println("多工作表 Excel 文件导出成功!")
}
```

### 3. 高性能大数据量导出（50万+行）

#### 方案1：流式写入（推荐⭐）

**适用场景**: 10万 - 100万行

流式写入通过**分批生成数据**，每次只加载一批到内存，避免内存溢出。

```go
// 模拟数据生成器
batchIndex := 0
rowGenerator := func() ([][]interface{}, error) {
    if batchIndex >= 100 { // 总共100批，每批5000行 = 50万行
        return nil, nil // 返回nil表示结束
    }
    
    // 生成一批数据（5000行）
    rows := make([][]interface{}, 5000)
    for i := 0; i < 5000; i++ {
        rows[i] = []interface{}{
            batchIndex*5000 + i + 1,
            fmt.Sprintf("用户%d", batchIndex*5000 + i + 1),
            20 + i%50,
        }
    }
    batchIndex++
    return rows, nil
}

// 流式导出（每次只加载5000行到内存）
f, err := goexcel.ExportLargeDataset(
    context.Background(),
    "大数据表",
    []string{"ID", "姓名", "年龄"},
    rowGenerator,
    5000, // 批次大小
)
if err != nil {
    log.Fatal(err)
}
defer f.Close()

if err := f.SaveAs("large_data.xlsx"); err != nil {
    log.Fatal(err)
}
```

**优势：**
- ✅ 内存占用低：每次只加载一批数据
- ✅ 支持无限行数：不受内存限制
- ✅ 性能优秀：50万行约需 750ms

**实际业务示例（从数据库读取）**：

```go
func exportUsersToExcel(ctx context.Context, db *sql.DB) error {
    // 查询总数
    var total int
    if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&total); err != nil {
        return err
    }
    
    batchSize := 5000
    offset := 0
    
    rowGenerator := func() ([][]interface{}, error) {
        if offset >= total {
            return nil, nil
        }
        
        // 从数据库读取一批数据
        query := `SELECT id, name, age, email FROM users LIMIT ? OFFSET ?`
        rows, err := db.Query(query, batchSize, offset)
        if err != nil {
            return nil, err
        }
        defer rows.Close()
        
        var result [][]interface{}
        for rows.Next() {
            var (
                id    int
                name  string
                age   int
                email string
            )
            if err := rows.Scan(&id, &name, &age, &email); err != nil {
                return nil, err
            }
            result = append(result, []interface{}{id, name, age, email})
        }
        
        offset += len(result)
        return result, nil
    }
    
    f, err := goexcel.ExportLargeDataset(
        ctx,
        "用户数据",
        []string{"ID", "姓名", "年龄", "邮箱"},
        rowGenerator,
        batchSize,
    )
    if err != nil {
        return err
    }
    defer f.Close()
    
    return f.SaveAs(fmt.Sprintf("users_%d.xlsx", time.Now().Unix()))
}
```

**性能指标**:
- 10万行: ~150ms, 内存 < 100MB
- 50万行: ~750ms, 内存 < 200MB
- 100万行: ~1.5s, 内存 < 300MB

#### 方案2：分割成多个工作表

**适用场景**: 数据量超过100万行，需要避免单Sheet行数超限

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/18721889353/sunshine/pkg/goexcel"
)

func main() {
    // 模拟生成50万行数据
    fmt.Println("正在生成测试数据...")
    allRows := make([][]interface{}, 500000)
    for i := 0; i < 500000; i++ {
        allRows[i] = []interface{}{
            i + 1,
            fmt.Sprintf("用户%d", i + 1),
            fmt.Sprintf("user%d@example.com", i + 1),
            20 + (i % 50),
            fmt.Sprintf("备注信息-%d", i + 1),
        }
    }
    fmt.Printf("已生成 %d 行数据\n", len(allRows))

    headers := []string{"ID", "姓名", "邮箱", "年龄", "备注"}

    // 分割成5个工作表，每个10万行
    maxRowsPerSheet := 100000
    sheets := goexcel.SplitIntoSheets("用户数据", headers, allRows, maxRowsPerSheet)
    
    fmt.Printf("数据已分割为 %d 个工作表\n", len(sheets))
    for i, sheet := range sheets {
        fmt.Printf("  - Sheet %d: %s (%d 行)\n", i + 1, sheet.SheetName, len(sheet.Rows))
    }

    // 导出多工作表 Excel
    f, err := goexcel.ExportMultiSheetExcel(context.Background(), sheets)
    if err != nil {
        log.Fatalf("导出失败: %v", err)
    }
    defer f.Close()

    filename := "split_users.xlsx"
    if err := f.SaveAs(filename); err != nil {
        log.Fatalf("保存失败: %v", err)
    }
    
    fmt.Printf("✅ 成功导出到 %s\n", filename)
}
```

**输出结果:**
```
正在生成测试数据...
已生成 500000 行数据
数据已分割为 5 个工作表
  - Sheet 1: 用户数据_1 (100000 行)
  - Sheet 2: 用户数据_2 (100000 行)
  - Sheet 3: 用户数据_3 (100000 行)
  - Sheet 4: 用户数据_4 (100000 行)
  - Sheet 5: 用户数据_5 (100000 行)
✅ 成功导出到 split_users.xlsx
```

**优势：**
- ✅ 避免单 Sheet 行数超限（Excel 限制 1048576 行）
- ✅ 便于数据分类和管理
- ✅ 打开速度更快

**缺点**:
- ❌ 需要一次性加载所有数据到内存

---

### 4. 高级用法

#### 4.1 大数据量导出（< 10万行）

**适用场景**: 中等规模数据，不需要流式处理

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/18721889353/sunshine/pkg/goexcel"
)

func main() {
    // 定义表头
    headers := []string{"ID", "姓名", "分数", "等级", "备注"}
    
    // 生成 1000 行学生成绩数据
    rows := make([][]interface{}, 1000)
    for i := 0; i < 1000; i++ {
        score := 60 + (i % 41) // 60-100分
        var grade string
        if score >= 90 {
            grade = "A"
        } else if score >= 80 {
            grade = "B"
        } else if score >= 70 {
            grade = "C"
        } else {
            grade = "D"
        }
        
        rows[i] = []interface{}{
            i + 1,
            fmt.Sprintf("学生%d", i + 1),
            score,
            grade,
            fmt.Sprintf("第%d学期", (i%4)+1),
        }
    }

    f, err := goexcel.ExportExcel(context.Background(), "成绩表", headers, rows)
    if err != nil {
        log.Fatalf("导出失败: %v", err)
    }
    defer f.Close()

    if err := f.SaveAs("students.xlsx"); err != nil {
        log.Fatalf("保存失败: %v", err)
    }
    
    log.Printf("成功导出 %d 条学生成绩", len(rows))
}
```

---

#### 4.2 多列导出（超过26列）

**适用场景**: 需要导出宽表格，列数超过Z列

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/18721889353/sunshine/pkg/goexcel"
)

func main() {
    // 创建 30 列（会自动使用 AA, AB 等列名）
    numCols := 30
    headers := make([]string, numCols)
    for i := 0; i < numCols; i++ {
        headers[i] = fmt.Sprintf("指标%d", i+1)
    }

    // 生成测试数据
    rows := [][]interface{}{
        make([]interface{}, numCols),
    }
    for i := 0; i < numCols; i++ {
        rows[0][i] = fmt.Sprintf("值%d", i+1)
    }

    f, err := goexcel.ExportExcel(context.Background(), "多列表格", headers, rows)
    if err != nil {
        log.Fatalf("导出失败: %v", err)
    }
    defer f.Close()

    if err := f.SaveAs("many_columns.xlsx"); err != nil {
        log.Fatalf("保存失败: %v", err)
    }
    
    log.Printf("成功导出 %d 列数据", numCols)
}
```

**生成的列名:** A, B, C, ..., Z, AA, AB, AC, AD, AE

---

#### 4.3 混合数据类型

**适用场景**: 同一列包含不同类型的数据

```go
package main

import (
    "context"
    "log"
    "github.com/18721889353/sunshine/pkg/goexcel"
)

func main() {
    headers := []string{"字符串", "整数", "浮点数", "布尔值", "空值"}
    
    rows := [][]interface{}{
        {"文本", 123, 45.67, true, nil},
        {"", 0, 0.0, false, "非空"},
        {"中文", -100, -3.14, true, 123},
        {"Emoji 😊", 999999, 3.1415926, false, ""},
    }

    f, err := goexcel.ExportExcel(context.Background(), "混合类型", headers, rows)
    if err != nil {
        log.Fatalf("导出失败: %v", err)
    }
    defer f.Close()

    if err := f.SaveAs("mixed_types.xlsx"); err != nil {
        log.Fatalf("保存失败: %v", err)
    }
    
    log.Println("成功导出混合类型数据")
}
```

**支持的数据类型:**
- ✅ 字符串 (string)
- ✅ 整数 (int, int8, int16, int32, int64)
- ✅ 浮点数 (float32, float64)
- ✅ 布尔值 (bool)
- ✅ 空值 (nil)
- ✅ 特殊字符 (中文、emoji、换行符等)

---

### 5. 实战案例 - 从数据库导出

**场景**: 从MySQL数据库导出用户数据到Excel

```go
package main

import (
    "database/sql"
    "fmt"
    "log"
    "time"
    
    _ "github.com/go-sql-driver/mysql"
    "github.com/18721889353/sunshine/pkg/goexcel"
)

// 用户结构体
type User struct {
    ID        int
    Name      string
    Email     string
    Age       int
    CreatedAt time.Time
}

func exportUsersToExcel(db *sql.DB) error {
    // 1. 查询总数
    var total int
    if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&total); err != nil {
        return fmt.Errorf("查询总数失败: %w", err)
    }
    
    if total == 0 {
        return fmt.Errorf("没有数据可导出")
    }
    
    log.Printf("共有 %d 条用户数据", total)
    
    // 2. 定义批次大小
    batchSize := 5000
    offset := 0
    exportedCount := 0
    
    // 3. 创建数据生成器
    rowGenerator := func() ([][]interface{}, error) {
        if offset >= total {
            return nil, nil // 返回nil表示结束
        }
        
        // 从数据库读取一批数据
        query := `SELECT id, name, email, age, created_at 
                  FROM users 
                  ORDER BY id 
                  LIMIT ? OFFSET ?`
        
        rows, err := db.Query(query, batchSize, offset)
        if err != nil {
            return nil, fmt.Errorf("查询失败: %w", err)
        }
        defer rows.Close()
        
        var result [][]interface{}
        for rows.Next() {
            var user User
            if err := rows.Scan(&user.ID, &user.Name, &user.Email, &user.Age, &user.CreatedAt); err != nil {
                return nil, fmt.Errorf("扫描行失败: %w", err)
            }
            
            result = append(result, []interface{}{
                user.ID,
                user.Name,
                user.Email,
                user.Age,
                user.CreatedAt.Format("2006-01-02 15:04:05"),
            })
        }
        
        if err := rows.Err(); err != nil {
            return nil, fmt.Errorf("遍历行失败: %w", err)
        }
        
        offset += len(result)
        exportedCount += len(result)
        
        // 进度提示
        if exportedCount%10000 == 0 || exportedCount == total {
            progress := float64(exportedCount) / float64(total) * 100
            log.Printf("已导出 %d/%d (%.1f%%)", exportedCount, total, progress)
        }
        
        return result, nil
    }
    
    // 4. 流式导出
    f, err := goexcel.ExportLargeDataset(
        "用户数据",
        []string{"ID", "姓名", "邮箱", "年龄", "注册时间"},
        rowGenerator,
        batchSize,
    )
    if err != nil {
        return fmt.Errorf("导出失败: %w", err)
    }
    defer f.Close()
    
    // 5. 保存文件
    filename := fmt.Sprintf("users_%s.xlsx", time.Now().Format("20060102_150405"))
    if err := f.SaveAs(filename); err != nil {
        return fmt.Errorf("保存失败: %w", err)
    }
    
    log.Printf("✅ 成功导出 %d 条用户数据到 %s", exportedCount, filename)
    return nil
}

func main() {
    // 连接数据库
    db, err := sql.Open("mysql", "user:password@tcp(localhost:3306)/dbname")
    if err != nil {
        log.Fatalf("连接数据库失败: %v", err)
    }
    defer db.Close()
    
    // 导出数据
    if err := exportUsersToExcel(db); err != nil {
        log.Fatalf("导出失败: %v", err)
    }
}
```

**输出示例:**
```
2026/05/21 10:45:00 共有 50000 条用户数据
2026/05/21 10:45:01 已导出 10000/50000 (20.0%)
2026/05/21 10:45:02 已导出 20000/50000 (40.0%)
2026/05/21 10:45:03 已导出 30000/50000 (60.0%)
2026/05/21 10:45:04 已导出 40000/50000 (80.0%)
2026/05/21 10:45:05 已导出 50000/50000 (100.0%)
2026/05/21 10:45:05 ✅ 成功导出 50000 条用户数据到 users_20260521_104505.xlsx
```

**关键要点:**
- ✅ 使用流式写入，避免内存溢出
- ✅ 分批查询数据库，每次只加载5000行
- ✅ 实时显示导出进度
- ✅ 完善的错误处理
- ✅ 文件名包含时间戳，便于管理

---


## 📋 API 参考

### ExportExcel - 基础导出

```go
func ExportExcel(ctx context.Context, sheetName string, headers []string, rows [][]interface{}) (*excelize.File, error)
```

**参数:**
- `sheetName`: 工作表名称（⚠️ 不要使用 "sheet1" 这种默认名称）
- `headers`: 表头切片，定义列名
- `rows`: 数据切片，二维数组，每一行是一个 `[]interface{}`

**返回:**
- `*excelize.File`: Excel 文件对象，使用后需要调用 `Close()` 释放资源
- `error`: 错误信息

---

### ExportMultiSheetExcel - 多工作表导出

```go
func ExportMultiSheetExcel(ctx context.Context, sheets []SheetData) (*excelize.File, error)
```

**参数:**
- `sheets`: 工作表数据列表

**SheetData 结构:**
```go
type SheetData struct {
    SheetName string        // 工作表名称
    Headers   []string      // 表头
    Rows      [][]interface{} // 数据行
}
```

---

### ExportLargeDataset - 流式写入（高性能）

```go
func ExportLargeDataset(
    ctx context.Context,
    sheetName string,
    headers []string,
    rowGenerator func() ([][]interface{}, error),
    batchSize int,
) (*excelize.File, error)
```

**参数:**
- `sheetName`: 工作表名称
- `headers`: 表头
- `rowGenerator`: 数据生成器函数，每次调用返回一批数据
- `batchSize`: 每批数据量（建议 5000-10000）

---

### SplitIntoSheets - 数据分割

```go
func SplitIntoSheets(
    sheetNamePrefix string,
    headers []string,
    allRows [][]interface{},
    maxRowsPerSheet int,
) []SheetData
```

**参数:**
- `sheetNamePrefix`: 工作表名称前缀（会自动添加序号）
- `headers`: 表头
- `allRows`: 全部数据
- `maxRowsPerSheet`: 每个工作表最大行数（建议 50000-100000）

## 🧪 测试

### 运行所有测试

```bash
cd D:/go/src/sunshine/pkg/goexcel
go test -v
```

### 运行性能基准测试

```bash
go test -bench=. -benchmem
```

**性能指标:**
- 100行 × 5列: ~1ms/op, ~485KB 内存分配
- 10万行（流式）: ~150ms/op, ~91MB 内存分配
- 50万行（流式）: ~750ms，内存占用 <200MB
- 适用于中小规模到大规模数据导出场景

### 查看测试覆盖率

```bash
go test -coverprofile=coverage.out
go tool cover -func=coverage.out
```

**当前覆盖率:** 82.8%

### 生成的测试文件

测试会在 `files/` 目录生成示例文件：

```
pkg/goexcel/files/
── employee_list.xlsx              # 员工列表示例（4行）
├── large_data_test.xlsx            # 大数据量测试（1000行）
├── large_dataset_streaming.xlsx    # 流式导出测试（10万行）
├── many_columns_test.xlsx          # 多列测试（30列）
├── mixed_types_test.xlsx           # 混合类型测试（3行）
├── multi_sheet_test.xlsx           # 多工作表测试（3个Sheet）
├── special_chars_test.xlsx         # 特殊字符测试（3行）
└── split_sheets_test.xlsx          # 数据分割测试（10万行分4个Sheet）
```

所有生成的文件**都不包含空的Sheet1**。

## ⚠️ 注意事项

1. **工作表命名**: 可以任意命名工作表，库会自动处理默认Sheet1的重命名
2. **无空Sheet问题**: 所有导出函数都已修复，不会生成多余的空Sheet1
3. **资源释放**: 使用完 Excel 文件后务必调用 `Close()` 方法
4. **内存使用**: 
   - 小数据量（<10万行）：直接使用 `ExportExcel`
   - 大数据量（10万-100万行）：使用 `ExportLargeDataset` 流式写入
   - 超大数据量（>100万行）：使用 `SplitIntoSheets` 分割成多个 Sheet
5. **并发安全**: `ExportExcel` 函数是线程安全的，可以在多个 goroutine 中同时调用
6. **错误处理**: 始终检查返回的 error，确保导出成功
7. **Excel 限制**: 单个 Sheet 最多 1,048,576 行，超过需分割

---

## 💡 最佳实践

### 1. 批次大小选择

```go
// 小数据量 (< 10万行)
batchSize := 10000

// 中等数据量 (10万 - 50万行)
batchSize := 5000

// 大数据量 (> 50万行)
batchSize := 2000
```

**原则**:
- 批次越大，性能越好，但内存占用越高
- 建议范围：2000 - 10000

### 2. 内存优化技巧

```go
// ✅ 好的做法：预分配切片
rows := make([][]interface{}, 0, batchSize)

// ❌ 不好的做法：动态扩容
var rows [][]interface{}
```

### 3. 进度监控

```go
totalExported := 0
rowGenerator := func() ([][]interface{}, error) {
    // ... 生成数据 ...
    
    totalExported += len(result)
    if totalExported%10000 == 0 {
        log.Printf("已导出 %d 行", totalExported)
    }
    
    return result, nil
}
```

### 4. 错误处理

```go
rowGenerator := func() ([][]interface{}, error) {
    // 数据库查询
    rows, err := db.Query(...)
    if err != nil {
        return nil, fmt.Errorf("查询失败: %w", err)
    }
    
    // ... 处理数据 ...
    
    return result, nil
}
```

### 5. 导出过程中如何取消？

```go
// 使用 context 控制
ctx, cancel := context.WithCancel(context.Background())

rowGenerator := func() ([][]interface{}, error) {
    // 检查是否取消
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    
    // ... 生成数据 ...
}

// 取消导出
cancel()
```

---

## 🔧 常见问题

### Q1: 如何处理特殊字符？

goexcel 自动处理特殊字符，包括：
- 中文、日文、韩文
- Emoji 😊
- 换行符、制表符
- SQL注入字符

无需额外处理。

### Q2: 能否追加到已有Excel？

当前版本不支持。如需此功能，可以：
1. 读取现有Excel
2. 解析出已有数据
3. 合并新数据
4. 重新保存

### Q3: 如何设置单元格样式？

当前版本专注于数据导出，暂不支持样式。
如需样式，可直接使用 `excelize` 库：

```go
f, _ := goexcel.ExportExcel(context.Background(), "Sheet1", headers, rows)

// 设置样式
style, _ := f.NewStyle(&excelize.Style{
    Font: &excelize.Font{Bold: true},
})
f.SetCellStyle("Sheet1", "A1", "D1", style)
```

### Q4: 性能调优建议

1. **减少列数**: 列数越多，性能越差
2. **简化数据类型**: 字符串比数字更耗内存
3. **合理分批**: 找到性能和内存的平衡点
4. **异步导出**: 大数据量时使用 goroutine 异步处理

## 📈 性能测试

### 基准测试结果

```bash
# 100行 × 5列
BenchmarkExportExcel-8    1200    971397 ns/op    484872 B/op    5147 allocs/op

# 10万行流式导出
BenchmarkExportLargeDataset-8    7    148966200 ns/op    91259100 B/op    953096 allocs/op
```

**性能指标:**
- 100行 × 5列: ~1ms, ~485KB 内存
- 1000行: ~10ms, ~31KB 文件
- 10万行（流式）: ~150ms, <100MB 内存
- 50万行（流式）: ~750ms, <200MB 内存
- 100万行（流式）: ~1.5s, <300MB 内存

## 🔍 内部实现

### 列名生成算法

Excel 的列名规则：
- 0-25: A-Z
- 26-701: AA-AZ, BA-BZ, ..., ZA-ZZ
- 702+: AAA, AAB, ...

使用递归算法高效生成列名，并预分配内存以避免多次分配。

### 内存优化

- 预计算最大列名长度，一次性分配足够容量
- 复用字节切片生成单元格名称，减少内存分配
- 使用 `strconv.AppendInt` 直接追加到字节切片

## 📝 更新日志

- **v2.0.0** (2026-05-21): 大厂标准升级
  - 所有公共方法添加 `context.Context` 参数
  - 集成 OpenTelemetry 链路追踪
  - 使用结构化日志记录
  - 符合大厂代码规范
  - 更新所有测试和文档
- **v1.1.1** (2026-05-21): Bug修复
  - 修复所有导出函数生成空Sheet1的问题
  - 改用重命名默认Sheet而非创建新Sheet
  - 确保生成的文件没有多余的空工作表
  - 优化测试用例，验证无空Sheet
- **v1.1.0** (2026-05-21): 高性能大数据量支持
  - 新增 `ExportMultiSheetExcel` 多工作表导出
  - 新增 `ExportLargeDataset` 流式写入（支持50万+行）
  - 新增 `SplitIntoSheets` 数据分割功能
  - 性能优化：10万行约150ms，50万行约750ms
  - 内存优化：流式写入，避免OOM
- **v1.0.0** (2026-05-21): 初始版本
  - 支持基本的 Excel 导出功能
  - 自动处理列名映射
  - 完整的单元测试和基准测试
  - 代码覆盖率 82.8%

## 📄 许可证

遵循项目主许可证

---

**相关文档**: 
- [excelize 官方文档](https://xuri.me/excelize/zh-hans/)
- [excel.go](file://D:/go/src/sunshine/pkg/goexcel/excel.go)
- [excel_test.go](file://D:/go/src/sunshine/pkg/goexcel/excel_test.go)
