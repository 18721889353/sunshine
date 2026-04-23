package exceltool

import (
	"github.com/18721889353/sunshine/pkg/dump"
	"github.com/xuri/excelize/v2"
	"testing"
)

func TestExcel(t *testing.T) {
	headers := []string{"用户名", "性别", "年龄"}
	values := [][]interface{}{
		{"测试", "1", "男"},
		{"ggr1", "1", "男"},
	}
	f, err := ExportExcel("sheet1", headers, values)
	defer func(f *excelize.File) {
		err := f.Close()
		if err != nil {

		}
	}(f)
	if err != nil {
		dump.P(err)
	}
	err = f.SaveAs("./static/export/test.xlsx")
	if err != nil {
		dump.P(err.Error())
	}
}
