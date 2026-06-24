// Package goexcel 提供 Excel 文件处理工具。
//
// 功能特性:
//   - 支持导出Excel文件（带表头和数据）
//   - 自动处理列名映射（A-Z, AA-AZ, AAA等）
//   - 支持多工作表
//   - 支持流式写入（高性能，适合大数据量）
//   - 支持分批导出（避免内存溢出）
//   - 线程安全
//
// 使用示例:
//
//	headers := []string{"姓名", "年龄", "城市"}
//	rows := [][]interface{}{
//	    {"张三", 25, "北京"},
//	    {"李四", 30, "上海"},
//	}
//	f, err := goexcel.ExportExcel("用户列表", headers, rows)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer f.Close()
//	if err := f.SaveAs("output.xlsx"); err != nil {
//	    log.Fatal(err)
//	}
package goexcel

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("excel.request_id", reqID)
		}
	}
	return attribute.String("excel.request_id", "")
}

// maxCharCount Excel列名最大字符数（A-Z共26个字母）
const maxCharCount = 26

// ExportExcel 导出Excel文件
//
// 参数:
//   - sheetName: 工作表名称（注意：不要使用sheet1这种默认名称，可能导致文件打开错误）
//   - headers: 表头切片，定义列名
//   - rows: 数据切片，二维数组，每一行是一个[]interface{}
//
// 返回:
//   - *excelize.File: Excel文件对象，使用后需要调用Close()释放资源
//   - error: 错误信息
//
// 使用示例:
//
//	headers := []string{"用户名", "性别", "年龄"}
//	rows := [][]interface{}{
//	    {"张三", "男", 25},
//	    {"李四", "女", 30},
//	}
//	f, err := goexcel.ExportExcel("用户信息", headers, rows)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer f.Close()
//	if err := f.SaveAs("users.xlsx"); err != nil {
//	    log.Fatal(err)
//	}
func ExportExcel(ctx context.Context, sheetName string, headers []string, rows [][]interface{}) (*excelize.File, error) {
	// 链路追踪
	tracer := otel.Tracer("goexcel")
	spanName := "excel.export.single"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "Start exporting Excel file",
		logger.String("sheet_name", sheetName),
		logger.Int("headers_count", len(headers)),
		logger.Int("rows_count", len(rows)))

	startTime := time.Now()

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("excel.sheet.name", sheetName),
		attribute.Int("excel.headers.count", len(headers)),
		attribute.Int("excel.rows.count", len(rows)),
		requestIDAttr(ctx),
	)

	if sheetName == "" {
		err := fmt.Errorf("工作表名称不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if headers == nil || len(headers) == 0 {
		err := fmt.Errorf("表头不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	f := excelize.NewFile()

	// 重命名默认的Sheet1为指定名称
	defaultSheet := f.GetSheetName(0)
	if err := f.SetSheetName(defaultSheet, sheetName); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("重命名默认工作表失败: %w", err)
	}
	sheetIndex := 0 // 重命名后的Sheet索引为0

	// 计算列名最大长度，用于预分配内存
	maxColumnRowNameLen := calculateMaxColumnRowNameLen(len(rows), len(headers))

	// 生成所有列名
	columnNames := generateColumnNames(headers, maxColumnRowNameLen)

	// 设置表头（第一行）
	if err := setHeaders(f, sheetName, columnNames, headers); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// 设置数据行（从第二行开始）
	if err := setDataRows(f, sheetName, columnNames, rows); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// 设置活动工作表
	f.SetActiveSheet(sheetIndex)

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Float64("excel.export.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "excel exported successfully")

	return f, nil
}

// SheetData 工作表数据结构
type SheetData struct {
	SheetName string          // 工作表名称
	Headers   []string        // 表头
	Rows      [][]interface{} // 数据行
}

// ExportMultiSheetExcel 导出多工作表Excel文件
//
// 参数:
//   - sheets: 工作表数据列表
//
// 返回:
//   - *excelize.File: Excel文件对象
//   - error: 错误信息
//
// 使用示例:
//
//	sheets := []goexcel.SheetData{
//	    {
//	        SheetName: "用户列表",
//	        Headers: []string{"姓名", "年龄"},
//	        Rows: [][]interface{}{
//	            {"张三", 25},
//	            {"李四", 30},
//	        },
//	    },
//	    {
//	        SheetName: "订单列表",
//	        Headers: []string{"订单号", "金额"},
//	        Rows: [][]interface{}{
//	            {"ORD001", 100},
//	            {"ORD002", 200},
//	        },
//	    },
//	}
//	f, err := goexcel.ExportMultiSheetExcel(sheets)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer f.Close()
//	if err := f.SaveAs("multi_sheet.xlsx"); err != nil {
//	    log.Fatal(err)
//	}
func ExportMultiSheetExcel(ctx context.Context, sheets []SheetData) (*excelize.File, error) {
	// 链路追踪
	tracer := otel.Tracer("goexcel")
	spanName := "excel.export.multi_sheet"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "Start exporting multi-sheet Excel file",
		logger.Int("sheets_count", len(sheets)))

	startTime := time.Now()

	// 设置追踪属性
	span.SetAttributes(
		attribute.Int("excel.sheets.count", len(sheets)),
		requestIDAttr(ctx),
	)

	if len(sheets) == 0 {
		err := fmt.Errorf("工作表数据不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	f := excelize.NewFile()

	// 重命名默认的Sheet1为第一个工作表的名称
	if len(sheets) > 0 {
		defaultSheet := f.GetSheetName(0)
		if err := f.SetSheetName(defaultSheet, sheets[0].SheetName); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("重命名默认工作表失败: %w", err)
		}
	}

	for i, sheet := range sheets {
		if err := validateSheetData(sheet); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("工作表[%d]数据验证失败: %w", i, err)
		}

		// 第一个工作表已经通过重命名默认Sheet创建
		var sheetIndex int
		if i == 0 {
			sheetIndex = 0 // 使用已重命名的默认Sheet
		} else {
			// 创建工作表
			var err error
			sheetIndex, err = f.NewSheet(sheet.SheetName)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return nil, fmt.Errorf("创建工作表[%s]失败: %w", sheet.SheetName, err)
			}
		}

		// 计算列名最大长度
		maxColumnRowNameLen := calculateMaxColumnRowNameLen(len(sheet.Rows), len(sheet.Headers))

		// 生成列名
		columnNames := generateColumnNames(sheet.Headers, maxColumnRowNameLen)

		// 设置表头
		if err := setHeaders(f, sheet.SheetName, columnNames, sheet.Headers); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("设置表头失败: %w", err)
		}

		// 设置数据行
		if err := setDataRows(f, sheet.SheetName, columnNames, sheet.Rows); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("设置数据行失败: %w", err)
		}

		// 设置第一个工作表为活动工作表
		if i == 0 {
			f.SetActiveSheet(sheetIndex)
		}
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Float64("excel.export.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "excel exported successfully")

	return f, nil
}

// ExportLargeDataset 高性能导出大数据集（分批写入）
// 适用于 10 万+ 行数据的导出场景
//
// 参数:
//   - sheetName: 工作表名称
//   - headers: 表头
//   - rowGenerator: 数据生成器函数，每次调用返回一批数据
//   - batchSize: 每批数据量（建议 5000-10000）
//
// 返回:
//   - *excelize.File: Excel文件对象
//   - error: 错误信息
//
// 使用示例:
//
//	batchIndex := 0
//	rowGenerator := func() ([][]interface{}, error) {
//	    if batchIndex >= 100 {
//	        return nil, nil // 返回nil表示结束
//	    }
//	    // 生成一批数据（5000行）
//	    rows := make([][]interface{}, 5000)
//	    for i := 0; i < 5000; i++ {
//	        rows[i] = []interface{}{batchIndex*5000 + i, fmt.Sprintf("用户%d", batchIndex*5000+i)}
//	    }
//	    batchIndex++
//	    return rows, nil
//	}
//
//	f, err := goexcel.ExportLargeDataset(
//	    "大数据表",
//	    []string{"ID", "姓名"},
//	    rowGenerator,
//	    5000,
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer f.Close()
//	if err := f.SaveAs("large_data.xlsx"); err != nil {
//	    log.Fatal(err)
//	}
func ExportLargeDataset(
	ctx context.Context,
	sheetName string,
	headers []string,
	rowGenerator func() ([][]interface{}, error),
	batchSize int,
) (*excelize.File, error) {
	// 链路追踪
	tracer := otel.Tracer("goexcel")
	spanName := "excel.export.large_dataset"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "Start exporting large dataset",
		logger.String("sheet_name", sheetName),
		logger.Int("headers_count", len(headers)),
		logger.Int("batch_size", batchSize))

	startTime := time.Now()

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("excel.sheet.name", sheetName),
		attribute.Int("excel.headers.count", len(headers)),
		attribute.Int("excel.batch.size", batchSize),
		requestIDAttr(ctx),
	)

	if sheetName == "" {
		err := fmt.Errorf("工作表名称不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if headers == nil || len(headers) == 0 {
		err := fmt.Errorf("表头不能为空")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if batchSize <= 0 {
		batchSize = 5000 // 默认批次大小
	}

	f := excelize.NewFile()

	// 重命名默认的Sheet1为指定名称
	defaultSheet := f.GetSheetName(0)
	if err := f.SetSheetName(defaultSheet, sheetName); err != nil {
		return nil, fmt.Errorf("重命名默认工作表失败: %w", err)
	}
	sheetIndex := 0 // 重命名后的Sheet索引为0

	// 计算列名最大长度（预估100万行）
	maxColumnRowNameLen := calculateMaxColumnRowNameLen(1000000, len(headers))
	columnNames := generateColumnNames(headers, maxColumnRowNameLen)

	// 设置表头
	if err := setHeaders(f, sheetName, columnNames, headers); err != nil {
		return nil, err
	}

	// 分批写入数据
	currentRow := 2 // 从第2行开始（第1行是表头）
	totalRows := 0

	for {
		// 获取一批数据
		rows, err := rowGenerator()
		if err != nil {
			return nil, fmt.Errorf("生成数据失败: %w", err)
		}
		if rows == nil || len(rows) == 0 {
			break // 数据生成完毕
		}

		// 写入当前批次
		for _, row := range rows {
			for colIndex, columnName := range columnNames {
				if colIndex >= len(row) {
					break
				}
				cellName := getColumnRowName(columnName, currentRow)
				if err := f.SetCellValue(sheetName, cellName, row[colIndex]); err != nil {
					return nil, fmt.Errorf("设置数据[%d,%d]失败: %w", currentRow, colIndex, err)
				}
			}
			currentRow++
			totalRows++
		}

		// 可选：定期刷新，避免内存占用过高
		// if totalRows%10000 == 0 {
		//     log.Printf("已写入 %d 行", totalRows)
		// }
	}

	f.SetActiveSheet(sheetIndex)

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Float64("excel.export.duration_ms", float64(duration.Milliseconds())),
		attribute.Int("excel.total.rows", totalRows),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "excel exported successfully")

	return f, nil
}

// SplitIntoSheets 将大数据集分割成多个工作表
// 适用于单个Sheet行数超过Excel限制（1048576行）的场景
//
// 参数:
//   - sheetNamePrefix: 工作表名称前缀（会自动添加序号）
//   - headers: 表头
//   - allRows: 全部数据
//   - maxRowsPerSheet: 每个工作表最大行数（建议 50000-100000）
//
// 返回:
//   - []SheetData: 分割后的工作表数据列表
//
// 使用示例:
//
//	// 将50万行数据分割成5个工作表，每个10万行
//	sheets := goexcel.SplitIntoSheets("数据", headers, allRows, 100000)
//	f, err := goexcel.ExportMultiSheetExcel(sheets)
//	if err != nil {
//	    log.Fatal(err)
//	}
func SplitIntoSheets(ctx context.Context, sheetNamePrefix string, headers []string, allRows [][]interface{}, maxRowsPerSheet int) []SheetData {
	// 链路追踪
	tracer := otel.Tracer("goexcel")
	spanName := "excel.split_into_sheets"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "Start splitting data into multiple sheets",
		logger.String("sheet_name_prefix", sheetNamePrefix),
		logger.Int("total_rows", len(allRows)))

	if maxRowsPerSheet <= 0 {
		maxRowsPerSheet = 100000 // 默认每个Sheet 10万行
	}

	// 设置追踪属性
	span.SetAttributes(
		attribute.String("excel.sheet_name_prefix", sheetNamePrefix),
		attribute.Int("excel.total_rows", len(allRows)),
		attribute.Int("excel.max_rows_per_sheet", maxRowsPerSheet),
		requestIDAttr(ctx),
	)

	totalRows := len(allRows)
	sheetCount := (totalRows + maxRowsPerSheet - 1) / maxRowsPerSheet // 向上取整

	sheets := make([]SheetData, 0, sheetCount)

	for i := 0; i < sheetCount; i++ {
		startIdx := i * maxRowsPerSheet
		endIdx := startIdx + maxRowsPerSheet
		if endIdx > totalRows {
			endIdx = totalRows
		}

		sheetName := fmt.Sprintf("%s_%d", sheetNamePrefix, i+1)
		sheets = append(sheets, SheetData{
			SheetName: sheetName,
			Headers:   headers,
			Rows:      allRows[startIdx:endIdx],
		})
	}

	// 设置成功的追踪属性
	span.SetAttributes(
		attribute.Int("excel.resulting_sheet_count", sheetCount),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "split completed successfully")

	return sheets
}

// validateSheetData 验证工作表数据
func validateSheetData(sheet SheetData) error {
	if sheet.SheetName == "" {
		return fmt.Errorf("工作表名称不能为空")
	}
	if sheet.Headers == nil || len(sheet.Headers) == 0 {
		return fmt.Errorf("表头不能为空")
	}
	return nil
}

// calculateMaxColumnRowNameLen 计算名称框的最大长度
// 例如：10行1000列的数据，最后一个名称框是J1001（含表头），长度为5
func calculateMaxColumnRowNameLen(rowCount, columnCount int) int {
	// 基础长度：1个列字母 + 行号位数（行数+1是因为包含表头行）
	baseLen := 1 + len(strconv.Itoa(rowCount+1))

	// 根据列数调整列字母长度
	if columnCount > maxCharCount {
		baseLen++ // AA, AB, ... (2个字母)
	} else if columnCount > maxCharCount*maxCharCount {
		baseLen += 2 // AAA, AAB, ... (3个字母)
	}

	return baseLen
}

// generateColumnNames 生成所有列名
// 返回一个二维字节切片，每个元素是一个列名的字节切片
func generateColumnNames(headers []string, maxColumnRowNameLen int) [][]byte {
	columnNames := make([][]byte, 0, len(headers))
	for i := range headers {
		columnName := getColumnName(i, maxColumnRowNameLen)
		columnNames = append(columnNames, columnName)
	}
	return columnNames
}

// setHeaders 设置Excel表头（第一行）
func setHeaders(f *excelize.File, sheetName string, columnNames [][]byte, headers []string) error {
	for i, columnName := range columnNames {
		if i >= len(headers) {
			break
		}
		// 表头在第1行
		cellName := getColumnRowName(columnName, 1)
		if err := f.SetCellValue(sheetName, cellName, headers[i]); err != nil {
			return fmt.Errorf("设置表头[%d]失败: %w", i, err)
		}
	}
	return nil
}

// setDataRows 设置数据行（从第2行开始）
func setDataRows(f *excelize.File, sheetName string, columnNames [][]byte, rows [][]interface{}) error {
	for rowIndex, row := range rows {
		for colIndex, columnName := range columnNames {
			if colIndex >= len(row) {
				break // 当前行的列数不足，跳过
			}
			// 数据从第2行开始（第1行是表头）
			cellName := getColumnRowName(columnName, rowIndex+2)
			if err := f.SetCellValue(sheetName, cellName, row[colIndex]); err != nil {
				return fmt.Errorf("设置数据[%d,%d]失败: %w", rowIndex, colIndex, err)
			}
		}
	}
	return nil
}

// getColumnName 生成列名
//
// Excel的列名规则：
//   - 0-25: A-Z
//   - 26-701: AA-AZ, BA-BZ, ..., ZA-ZZ
//   - 702+: AAA, AAB, ...
//
// 参数:
//   - column: 列索引（从0开始）
//   - maxColumnRowNameLen: 名称框最大长度，用于预分配切片容量
//
// 返回:
//   - []byte: 列名的字节切片（如：A, B, ..., Z, AA, AB, ...）
func getColumnName(column, maxColumnRowNameLen int) []byte {
	const a = 'A'

	// 单字母列名（A-Z）
	if column < maxCharCount {
		slice := make([]byte, 0, maxColumnRowNameLen)
		return append(slice, byte(a+column))
	}

	// 多字母列名（AA, AB, ..., AAA, AAB, ...）
	// 递归生成：column/26-1 确定前缀，column%26 确定最后一个字母
	return append(
		getColumnName(column/maxCharCount-1, maxColumnRowNameLen),
		byte(a+column%maxCharCount),
	)
}

// getColumnRowName 生成单元格名称（名称框）
//
// Excel的单元格命名规则：列名 + 行号
//   - A1, B1, C1, ... （第1行）
//   - A2, B2, C2, ... （第2行）
//   - AA1, AB1, ... （超过26列后）
//
// 参数:
//   - columnName: 列名字节切片（如：A, B, AA）
//   - rowIndex: 行号（从1开始）
//
// 返回:
//   - string: 单元格名称（如：A1, B2, AA10）
func getColumnRowName(columnName []byte, rowIndex int) string {
	// 直接在列名后面追加行号，避免额外内存分配
	columnName = strconv.AppendInt(columnName, int64(rowIndex), 10)
	return string(columnName)
}
