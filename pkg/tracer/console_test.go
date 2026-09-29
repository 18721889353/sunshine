package tracer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewConsoleExporter 验证 NewConsoleExporter 创建控制台导出器成功。
func TestNewConsoleExporter(t *testing.T) {
	exporter, err := NewConsoleExporter()
	assert.NoError(t, err)
	assert.NotNil(t, exporter)
}

// TestNewFileExporter 验证 NewFileExporter 正常创建文件导出器（含空文件名默认值）以及非法路径返回错误。
func TestNewFileExporter(t *testing.T) {
	// 指定文件名
	path := filepath.Join(t.TempDir(), "demo.json")
	exporter, file, err := NewFileExporter(path)
	require.NoError(t, err)
	assert.NotNil(t, exporter)
	assert.NoError(t, file.Close())

	// 空文件名默认输出 traces.json（写入临时目录，避免在包目录残留文件）
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() {
		if chdirErr := os.Chdir(cwd); chdirErr != nil {
			t.Logf("恢复工作目录失败: %v", chdirErr)
		}
	})

	exporter, file, err = NewFileExporter("")
	require.NoError(t, err)
	assert.NotNil(t, exporter)
	assert.NoError(t, file.Close())
	_, err = os.Stat("traces.json")
	assert.NoError(t, err, "默认文件名应生成 traces.json")

	// 无效路径应返回 error（不 panic）
	_, _, err = NewFileExporter("\\\\")
	assert.Error(t, err)
}

// TestNewExporterWithWriter 验证 newExporter 向指定 Writer 输出 Span 导出器成功。
func TestNewExporterWithWriter(t *testing.T) {
	exporter, err := newExporter(os.Stdout)
	assert.NoError(t, err)
	assert.NotNil(t, exporter)
}
