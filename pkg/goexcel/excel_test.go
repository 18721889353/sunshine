package goexcel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestExportExcel 基础导出功能测试
func TestExportExcel(t *testing.T) {
	headers := []string{"用户名", "性别", "年龄"}
	rows := [][]interface{}{
		{"张三", "男", 25},
		{"李四", "女", 30},
		{"王五", "男", 28},
	}

	ctx := context.Background()
	f, err := ExportExcel(ctx, "用户信息", headers, rows)
	if err != nil {
		t.Fatalf("导出Excel失败: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("关闭Excel文件失败: %v", closeErr)
		}
	}()

	// 验证工作表是否存在（只有1个，因为重命名了默认Sheet）
	sheetList := f.GetSheetList()
	if len(sheetList) != 1 {
		t.Errorf("期望1个工作表，实际 %d", len(sheetList))
	}

	// 验证活动工作表（索引应该为0）
	activeIndex := f.GetActiveSheetIndex()
	if activeIndex != 0 {
		t.Errorf("活动工作表索引应为0，实际 %d", activeIndex)
	}
}

// TestExportExcelWithSave 导出并保存文件测试
func TestExportExcelWithSave(t *testing.T) {
	// 创建测试目录
	filesDir := "./files"
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	headers := []string{"姓名", "年龄", "城市"}
	rows := [][]interface{}{
		{"张三", 25, "北京"},
		{"李四", 30, "上海"},
		{"王五", 28, "广州"},
		{"赵六", 35, "深圳"},
	}

	ctx := context.Background()
	f, err := ExportExcel(ctx, "员工列表", headers, rows)
	if err != nil {
		t.Fatalf("导出Excel失败: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("关闭Excel文件失败: %v", closeErr)
		}
	}()

	// 保存文件
	filePath := filepath.Join(filesDir, "employee_list.xlsx")
	if err := f.SaveAs(filePath); err != nil {
		t.Fatalf("保存Excel文件失败: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("文件未生成: %s", filePath)
	}

	t.Logf("✅ Excel文件生成成功: %s", filePath)

	// 清理测试文件（可选，注释掉以便手动检查）
	// defer os.Remove(filePath)
}

// TestExportExcelEmptySheetName 空工作表名称测试
func TestExportExcelEmptySheetName(t *testing.T) {
	headers := []string{"列1", "列2"}
	rows := [][]interface{}{{"值1", "值2"}}

	ctx := context.Background()
	_, err := ExportExcel(ctx, "", headers, rows)
	if err == nil {
		t.Error("空工作表名称应该返回错误")
	}
}

// TestExportExcelEmptyHeaders 空表头测试
func TestExportExcelEmptyHeaders(t *testing.T) {
	rows := [][]interface{}{{"值1", "值2"}}

	ctx := context.Background()
	_, err := ExportExcel(ctx, "测试", []string{}, rows)
	if err == nil {
		t.Error("空表头应该返回错误")
	}

	_, err = ExportExcel(ctx, "测试", nil, rows)
	if err == nil {
		t.Error("nil表头应该返回错误")
	}
}

// TestExportExcelLargeData 大数据量测试
func TestExportExcelLargeData(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过大数据量测试")
	}

	// 创建1000行数据
	headers := []string{"ID", "姓名", "分数", "备注"}
	rows := make([][]interface{}, 1000)
	for i := 0; i < 1000; i++ {
		rows[i] = []interface{}{
			i + 1,
			fmt.Sprintf("学生%d", i+1),
			60 + i%41, // 60-100分
			fmt.Sprintf("备注信息-%d", i+1),
		}
	}

	ctx := context.Background()
	f, err := ExportExcel(ctx, "成绩表", headers, rows)
	if err != nil {
		t.Fatalf("导出大数据Excel失败: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("关闭Excel文件失败: %v", closeErr)
		}
	}()

	// 保存到files目录
	filesDir := "./files"
	filePath := filepath.Join(filesDir, "large_data_test.xlsx")
	if err := f.SaveAs(filePath); err != nil {
		t.Fatalf("保存Excel文件失败: %v", err)
	}

	// 验证文件大小
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("获取文件信息失败: %v", err)
	}

	t.Logf("✅ 大数据Excel生成成功: %s, 大小: %.2f KB", filePath, float64(fileInfo.Size())/1024)
}

// TestExportExcelManyColumns 多列测试
func TestExportExcelManyColumns(t *testing.T) {
	// 创建30列（超过26列，会用到AA, AB等列名）
	headers := make([]string, 30)
	for i := 0; i < 30; i++ {
		headers[i] = fmt.Sprintf("列%d", i+1)
	}

	rows := [][]interface{}{
		make([]interface{}, 30),
	}
	for i := 0; i < 30; i++ {
		rows[0][i] = fmt.Sprintf("值%d", i+1)
	}

	ctx := context.Background()
	f, err := ExportExcel(ctx, "多列测试", headers, rows)
	if err != nil {
		t.Fatalf("导出多列Excel失败: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("关闭Excel文件失败: %v", closeErr)
		}
	}()

	// 保存到files目录
	filesDir := "./files"
	filePath := filepath.Join(filesDir, "many_columns_test.xlsx")
	if err := f.SaveAs(filePath); err != nil {
		t.Fatalf("保存Excel文件失败: %v", err)
	}

	t.Logf("✅ 多列Excel生成成功: %s (共%d列)", filePath, len(headers))
}

// TestExportExcelSpecialCharacters 特殊字符测试
func TestExportExcelSpecialCharacters(t *testing.T) {
	headers := []string{"姓名", "邮箱", "描述"}
	rows := [][]interface{}{
		{"张三丰", "zhang@example.com", "这是一个测试描述，包含逗号、句号。"},
		{"Li Si", "li@test.com", "Special chars: @#$%^&*()"},
		{"王五", "wang@test.com", "换行\n测试\t制表符"},
	}

	ctx := context.Background()
	f, err := ExportExcel(ctx, "特殊字符", headers, rows)
	if err != nil {
		t.Fatalf("导出特殊字符Excel失败: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("关闭Excel文件失败: %v", closeErr)
		}
	}()

	// 保存到files目录
	filesDir := "./files"
	filePath := filepath.Join(filesDir, "special_chars_test.xlsx")
	if err := f.SaveAs(filePath); err != nil {
		t.Fatalf("保存Excel文件失败: %v", err)
	}

	t.Logf("✅ 特殊字符Excel生成成功: %s", filePath)
}

// TestExportExcelMixedTypes 混合数据类型测试
func TestExportExcelMixedTypes(t *testing.T) {
	headers := []string{"字符串", "整数", "浮点数", "布尔值", "nil值"}
	rows := [][]interface{}{
		{"文本", 123, 45.67, true, nil},
		{"", 0, 0.0, false, "非空"},
		{"中文", -100, -3.14, true, 123},
	}

	ctx := context.Background()
	f, err := ExportExcel(ctx, "混合类型", headers, rows)
	if err != nil {
		t.Fatalf("导出混合类型Excel失败: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("关闭Excel文件失败: %v", closeErr)
		}
	}()

	// 保存到files目录
	filesDir := "./files"
	filePath := filepath.Join(filesDir, "mixed_types_test.xlsx")
	if err := f.SaveAs(filePath); err != nil {
		t.Fatalf("保存Excel文件失败: %v", err)
	}

	t.Logf("✅ 混合类型Excel生成成功: %s", filePath)
}

// TestGetColumnName 列名生成测试
func TestGetColumnName(t *testing.T) {
	tests := []struct {
		column   int
		expected string
	}{
		{0, "A"},
		{1, "B"},
		{25, "Z"},
		{26, "AA"},
		{27, "AB"},
		{51, "AZ"},
		{52, "BA"},
		{701, "ZZ"},
		{702, "AAA"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Column%d", tt.column), func(t *testing.T) {
			result := getColumnName(tt.column, 10)
			if string(result) != tt.expected {
				t.Errorf("列 %d: 期望 %s, 实际 %s", tt.column, tt.expected, string(result))
			}
		})
	}
}

// TestGetColumnRowName 单元格名称生成测试
func TestGetColumnRowName(t *testing.T) {
	tests := []struct {
		columnName string
		rowIndex   int
		expected   string
	}{
		{"A", 1, "A1"},
		{"B", 2, "B2"},
		{"Z", 10, "Z10"},
		{"AA", 1, "AA1"},
		{"AB", 100, "AB100"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := getColumnRowName([]byte(tt.columnName), tt.rowIndex)
			if result != tt.expected {
				t.Errorf("列名 %s, 行 %d: 期望 %s, 实际 %s",
					tt.columnName, tt.rowIndex, tt.expected, result)
			}
		})
	}
}

// TestCalculateMaxColumnRowNameLen 计算最大长度测试
func TestCalculateMaxColumnRowNameLen(t *testing.T) {
	tests := []struct {
		rowCount    int
		columnCount int
		minLen      int // 最小期望长度
	}{
		{10, 5, 2},    // A11 (2位)
		{100, 20, 3},  // A101 (3位)
		{1000, 30, 5}, // AA1001 (5位)
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("R%dC%d", tt.rowCount, tt.columnCount), func(t *testing.T) {
			result := calculateMaxColumnRowNameLen(tt.rowCount, tt.columnCount)
			if result < tt.minLen {
				t.Errorf("行数=%d, 列数=%d: 期望至少%d, 实际%d",
					tt.rowCount, tt.columnCount, tt.minLen, result)
			}
		})
	}
}

// BenchmarkExportExcel 性能基准测试
func BenchmarkExportExcel(b *testing.B) {
	headers := []string{"ID", "姓名", "年龄", "城市", "分数"}
	rows := make([][]interface{}, 100)
	for i := 0; i < 100; i++ {
		rows[i] = []interface{}{
			i + 1,
			fmt.Sprintf("用户%d", i+1),
			20 + i%50,
			"北京",
			60 + i%41,
		}
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := ExportExcel(ctx, "性能测试", headers, rows)
		if err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
}

// ExampleExportExcel 使用示例
func ExampleExportExcel() {
	headers := []string{"姓名", "年龄", "城市"}
	rows := [][]interface{}{
		{"张三", 25, "北京"},
		{"李四", 30, "上海"},
	}

	ctx := context.Background()
	f, err := ExportExcel(ctx, "用户列表", headers, rows)
	if err != nil {
		return
	}
	defer f.Close()

	// 保存文件
	_ = f.SaveAs("example.xlsx")
	// 清理示例文件
	os.Remove("example.xlsx")
}

// TestExportMultiSheetExcel 多工作表导出测试
func TestExportMultiSheetExcel(t *testing.T) {
	sheets := []SheetData{
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
		{
			SheetName: "产品列表",
			Headers:   []string{"产品名", "价格", "库存"},
			Rows: [][]interface{}{
				{"产品A", 99.9, 100},
				{"产品B", 199.9, 50},
			},
		},
	}

	ctx := context.Background()
	f, err := ExportMultiSheetExcel(ctx, sheets)
	if err != nil {
		t.Fatalf("导出多工作表Excel失败: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("关闭Excel文件失败: %v", closeErr)
		}
	}()

	// 验证工作表数量（应该有3个工作表）
	sheetList := f.GetSheetList()
	if len(sheetList) != 3 {
		t.Errorf("期望3个工作表，实际 %d", len(sheetList))
	}

	// 保存到files目录
	filesDir := "./files"
	filePath := filepath.Join(filesDir, "multi_sheet_test.xlsx")
	if err := f.SaveAs(filePath); err != nil {
		t.Fatalf("保存Excel文件失败: %v", err)
	}

	t.Logf("✅ 多工作表Excel生成成功: %s (共%d个工作表)", filePath, len(sheetList))
}

// TestExportLargeDataset 大数据量流式导出测试
func TestExportLargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过大数据量测试")
	}

	// 模拟数据生成器（生成10万行数据）
	totalRows := 100000
	currentRow := 0
	batchSize := 5000

	rowGenerator := func() ([][]interface{}, error) {
		if currentRow >= totalRows {
			return nil, nil // 数据生成完毕
		}

		// 生成一批数据
		remaining := totalRows - currentRow
		if remaining > batchSize {
			remaining = batchSize
		}

		rows := make([][]interface{}, remaining)
		for i := 0; i < remaining; i++ {
			rows[i] = []interface{}{
				currentRow + i + 1,
				fmt.Sprintf("用户%d", currentRow+i+1),
				20 + (currentRow+i)%50,
				fmt.Sprintf("备注-%d", currentRow+i+1),
			}
		}

		currentRow += remaining
		return rows, nil
	}

	ctx := context.Background()
	f, err := ExportLargeDataset(
		ctx,
		"大数据表",
		[]string{"ID", "姓名", "年龄", "备注"},
		rowGenerator,
		batchSize,
	)
	if err != nil {
		t.Fatalf("导出大数据集失败: %v", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("关闭Excel文件失败: %v", closeErr)
		}
	}()

	// 保存到files目录
	filesDir := "./files"
	filePath := filepath.Join(filesDir, "large_dataset_streaming.xlsx")
	if err := f.SaveAs(filePath); err != nil {
		t.Fatalf("保存Excel文件失败: %v", err)
	}

	// 验证文件大小
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("获取文件信息失败: %v", err)
	}

	t.Logf("✅ 大数据集流式导出成功: %s, 大小: %.2f MB, 行数: %d",
		filePath, float64(fileInfo.Size())/(1024*1024), totalRows)
}

// TestSplitIntoSheets 数据分割测试
func TestSplitIntoSheets(t *testing.T) {
	// 创建10万行测试数据
	totalRows := 100000
	allRows := make([][]interface{}, totalRows)
	for i := 0; i < totalRows; i++ {
		allRows[i] = []interface{}{i + 1, fmt.Sprintf("数据%d", i+1)}
	}

	headers := []string{"ID", "内容"}
	maxRowsPerSheet := 30000

	ctx := context.Background()
	// 分割成多个工作表
	sheets := SplitIntoSheets(ctx, "数据", headers, allRows, maxRowsPerSheet)

	// 验证分割结果
	expectedSheets := 4 // 100000 / 30000 = 3.33，向上取整为4
	if len(sheets) != expectedSheets {
		t.Errorf("期望%d个工作表，实际 %d", expectedSheets, len(sheets))
	}

	// 验证每个工作表的行数
	for i, sheet := range sheets {
		expectedRows := maxRowsPerSheet
		if i == len(sheets)-1 {
			// 最后一个工作表可能不足
			expectedRows = totalRows - i*maxRowsPerSheet
		}

		if len(sheet.Rows) != expectedRows {
			t.Errorf("工作表[%d]期望%d行，实际%d行", i, expectedRows, len(sheet.Rows))
		}

		t.Logf("工作表[%d]: %s, 行数: %d", i, sheet.SheetName, len(sheet.Rows))
	}

	// 导出验证
	f, err := ExportMultiSheetExcel(ctx, sheets)
	if err != nil {
		t.Fatalf("导出分割后的Excel失败: %v", err)
	}
	defer f.Close()

	filesDir := "./files"
	filePath := filepath.Join(filesDir, "split_sheets_test.xlsx")
	if err := f.SaveAs(filePath); err != nil {
		t.Fatalf("保存Excel文件失败: %v", err)
	}

	t.Logf("✅ 数据分割导出成功: %s (共%d个工作表)", filePath, len(sheets))
}

// TestExportMultiSheetEmpty 空数据测试
func TestExportMultiSheetEmpty(t *testing.T) {
	// 测试空列表
	ctx := context.Background()
	_, err := ExportMultiSheetExcel(ctx, []SheetData{})
	if err == nil {
		t.Error("空工作表列表应该返回错误")
	}

	// 测试空表头
	sheets := []SheetData{
		{
			SheetName: "测试",
			Headers:   []string{},
			Rows:      [][]interface{}{{"值"}},
		},
	}
	_, err = ExportMultiSheetExcel(ctx, sheets)
	if err == nil {
		t.Error("空表头应该返回错误")
	}
}

// BenchmarkExportLargeDataset 大数据量性能基准测试
func BenchmarkExportLargeDataset(b *testing.B) {
	totalRows := 50000
	currentRow := 0
	batchSize := 5000

	rowGenerator := func() ([][]interface{}, error) {
		if currentRow >= totalRows {
			return nil, nil
		}

		remaining := totalRows - currentRow
		if remaining > batchSize {
			remaining = batchSize
		}

		rows := make([][]interface{}, remaining)
		for i := 0; i < remaining; i++ {
			rows[i] = []interface{}{
				currentRow + i + 1,
				fmt.Sprintf("用户%d", currentRow+i+1),
				20 + (currentRow+i)%50,
			}
		}

		currentRow += remaining
		return rows, nil
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		currentRow = 0 // 重置计数器
		f, err := ExportLargeDataset(
			ctx,
			"性能测试",
			[]string{"ID", "姓名", "年龄"},
			rowGenerator,
			batchSize,
		)
		if err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
}
