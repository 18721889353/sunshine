package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/18721889353/sunshine/pkg/sql2code"
)

// TestHTTPGenerator_generateCode 端到端测试：基于 cp_dealer_api_log 表生成完整 Web 服务代码
func TestHTTPGenerator_generateCode(t *testing.T) {
	dsn := "root:jianguo123@tcp(43.143.78.234:3306)/coupon_platform"
	tableName := "cp_dealer_api_log"

	tmpOut := filepath.Join(os.TempDir(), "sunshine-test", "http-gen")
	os.RemoveAll(tmpOut)
	testDataDir := filepath.Join("testdata", "http-gen")
	os.RemoveAll(testDataDir)

	// 通过 sql2code 连接数据库，获取表结构并生成代码
	sqlArgs := sql2code.Args{
		Package:       "model",
		DBDriver:      "mysql",
		DBDsn:         dsn,
		DBTable:       tableName,
		JSONTag:       true,
		JSONNamedType: 1,
		GormType:      true,
		IsExtendedAPI: false,
	}

	codes, err := sql2code.Generate(&sqlArgs)
	if err != nil {
		t.Fatalf("sql2code.Generate() 连接数据库解析表 %s 失败: %v", tableName, err)
	}

	t.Logf("解析结果: model=%d chars, dao=%d chars, handler=%d chars, crudInfo=%d chars, tableName=%s",
		len(codes["model"]), len(codes["dao"]), len(codes["handler"]), len(codes["crudInfo"]), codes["tableName"])

	gen := &httpGenerator{
		moduleName:     "github.com/myorg/coupon-platform",
		serverName:     "coupon",
		projectName:    "my-project",
		repoAddr:       "192.168.1.1:5000/myorg",
		dbDSN:          dsn,
		dbDriver:       "mysql",
		codes:          codes,
		outPath:        tmpOut,
		isEmbed:        false,
		isExtendedAPI:  false,
		suitedMonoRepo: false,
	}

	outPath, err := gen.generateCode()
	if err != nil {
		t.Fatalf("generateCode() 失败: %v", err)
	}
	t.Logf("生成输出目录: %s", outPath)

	// 查找生成的 Go 文件
	var gf string
	filepath.Walk(outPath, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			gf = path
		}
		return nil
	})
	if gf == "" {
		t.Fatal("未找到生成的 .go 文件")
	}
	t.Logf("生成的文件: %s", gf)

	// 复制到 testdata 目录供查看
	relPath, _ := filepath.Rel(tmpOut, gf)
	targetFile := filepath.Join(testDataDir, relPath)
	os.MkdirAll(filepath.Dir(targetFile), 0755)
	copyFile(t, gf, targetFile)
	t.Logf("已复制到: %s", targetFile)

	data, err := os.ReadFile(gf)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	content := string(data)

	// 验证占位符已被替换
	if strings.Contains(content, "userExample") || strings.Contains(content, "UserExample") {
		t.Error("生成文件不应包含原始模板名 userExample/UserExample")
	}
	if strings.Contains(content, "serverNameExample") {
		t.Error("生成文件不应包含原始模板名 serverNameExample")
	}

	// 列出所有生成的文件
	t.Log("--- 所有生成的文件 ---")
	filepath.Walk(outPath, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			rel, _ := filepath.Rel(outPath, path)
			dst := filepath.Join(testDataDir, rel)
			os.MkdirAll(filepath.Dir(dst), 0755)
			copyFile(t, path, dst)
			t.Logf("  %s", rel)
		}
		return nil
	})
}
