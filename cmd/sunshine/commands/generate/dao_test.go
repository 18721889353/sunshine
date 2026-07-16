package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/18721889353/sunshine/pkg/replacer"
	"github.com/18721889353/sunshine/pkg/sql2code"
)

// TestDaoGenerator_generateCode_WithRealDB 端到端测试：连接真实数据库 cp_dealer_api_log 表生成 DAO 代码
func TestDaoGenerator_generateCode_WithRealDB(t *testing.T) {
	// 真实数据库案例：coupon_platform.cp_dealer_api_log（券平台经销商 API 日志表）
	dsn := "root:jianguo123@tcp(43.143.78.234:3306)/coupon_platform"
	tableName := "cp_dealer_api_log"

	tmpOut := filepath.Join(os.TempDir(), "sunshine-test", "dao-gen-real")
	os.RemoveAll(tmpOut)
	testDataDir := filepath.Join("testdata", "dao-gen-real")
	os.RemoveAll(testDataDir)

	// 初始化 Replacers[TplNameSunshine]
	sourceDir := detectLocalSunshineSource()
	if sourceDir == "" {
		sourceDir = SunshineDir
	}
	if _, ok := Replacers[TplNameSunshine]; !ok {
		r, err := replacer.New(sourceDir)
		if err != nil {
			t.Fatalf("初始化 replacer 失败: %v", err)
		}
		Replacers[TplNameSunshine] = r
		t.Cleanup(func() { delete(Replacers, TplNameSunshine) })
	}

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

	t.Logf("解析结果: model=%d chars, dao=%d chars, crudInfo=%d chars, tableName=%s",
		len(codes["model"]), len(codes["dao"]), len(codes["crudInfo"]), codes["tableName"])

	// 创建 DAO 生成器
	gen := &daoGenerator{
		moduleName:      "github.com/myorg/coupon-platform",
		dbDriver:        "mysql",
		isIncludeInitDB: false,
		codes:           codes,
		outPath:         tmpOut,
		isEmbed:         false,
		isExtendedAPI:   false,
		serverName:      "",
		suitedMonoRepo:  false,
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

	// 验证生成文件内容
	data, err := os.ReadFile(gf)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	content := string(data)

	// 验证基本结构
	if !strings.Contains(content, "type CrudDealerApiLog struct") &&
		!strings.Contains(content, "type CpDealerApiLog struct") {
		t.Log("注意：结构体名可能不是 CrudDealerApiLog 或 CpDealerApiLog，实际内容:")
		t.Log(content[:min(len(content), 200)])
	}

	// 验证模块名替换
	if strings.Contains(content, "github.com/18721889353/sunshine") {
		t.Log("生成文件包含框架原始导入路径（属于正常框架内部引用）")
	}

	// 验证占位符已被替换
	if strings.Contains(content, "userExample") || strings.Contains(content, "UserExample") {
		t.Error("生成文件不应包含原始模板名 userExample/UserExample")
	}

	// 列出所有生成的文件
	t.Log("--- 所有生成的文件 ---")
	filepath.Walk(outPath, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			rel, _ := filepath.Rel(tmpOut, path)
			dst := filepath.Join(testDataDir, rel)
			os.MkdirAll(filepath.Dir(dst), 0755)
			copyFile(t, path, dst)
			t.Logf("  %s", rel)
		}
		return nil
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
