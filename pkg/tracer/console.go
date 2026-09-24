package tracer

import (
	"fmt"
	"io"
	"os"

	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewConsoleExporter 创建控制台输出的 Span 导出器（美化格式）。
func NewConsoleExporter() (sdkTrace.SpanExporter, error) {
	return stdouttrace.New(stdouttrace.WithPrettyPrint())
}

// NewFileExporter 创建文件输出的 Span 导出器。
// filename 为空时默认输出到 traces.json。
// 注意：结束追踪前需关闭返回的 *os.File。
func NewFileExporter(filename string) (sdkTrace.SpanExporter, *os.File, error) {
	if filename == "" {
		filename = "traces.json"
	}
	f, err := os.Create(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("创建 trace 文件 %q 失败: %w", filename, err)
	}

	exporter, err := newExporter(f)
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("创建控制台导出器失败: %w", err)
	}

	return exporter, f, nil
}

// newExporter 创建控制台输出的 Span 导出器。
func newExporter(w io.Writer) (sdkTrace.SpanExporter, error) {
	return stdouttrace.New(
		stdouttrace.WithWriter(w),
		// 美化输出格式
		stdouttrace.WithPrettyPrint(),
		// 不打印时间戳（示例用途）
		stdouttrace.WithoutTimestamps(),
	)
}
