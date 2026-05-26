package strutil

import (
	"strings"
	"testing"
)

// TestParseSafeFileName_Success 测试正常及带有各种特殊亚洲字符的清洗逻辑
func TestParseSafeFileName_Success(t *testing.T) {
	tests := []struct {
		name     string
		rawInput string
		expected string
	}{
		{
			name:     "用户提供的中文括号案例",
			rawInput: "1779765732_测试品牌（gcj）_测试卡券（gcj）_2元_41_80",
			expected: "1779765732_测试品牌_gcj_测试卡券_gcj_2元_41_80",
		},
		{
			name:     "带有中英文混合斜杠和空格的案例",
			rawInput: " 品牌 / 导出 ／ 卡券 (测试) ",
			expected: "品牌_导出_卡券_测试",
		},
		{
			name:     "包含系统保留字和连续下划线的案例",
			rawInput: "测试:*?_新建__文件夹/demo",
			expected: "测试_新建_文件夹_demo",
		},
		{
			name:     "纯中文全角空格的案例",
			rawInput: "　　测试品牌　　",
			expected: "测试品牌",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := ParseSafeFileName(tt.rawInput)
			if err != nil {
				t.Fatalf("期望清洗成功，但却抛出了异常: %v", err)
			}
			if actual != tt.expected {
				t.Errorf("清洗结果不符合预期！\n输入: %s\n期望: %s\n实际: %s", tt.rawInput, tt.expected, actual)
			}
		})
	}
}

// TestParseSafeFileName_Exception 测试当文件名全被滤空时，正确抛出异常的熔断逻辑
func TestParseSafeFileName_Exception(t *testing.T) {
	tests := []struct {
		name     string
		rawInput string
	}{
		{
			name:     "纯英文半角和中文全角空格",
			rawInput: "  　　  ",
		},
		{
			name:     "纯系统保留非法字符和斜杠",
			rawInput: `///\\\:::***???`,
		},
		{
			name:     "纯全角和半角括号",
			rawInput: "（）（）()()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := ParseSafeFileName(tt.rawInput)
			// 断言必须产生错误
			if err == nil {
				t.Fatalf("错误熔断失效！输入纯非法字符 [%s] 后仍返回了文件名 [%s]，应该抛出异常！", tt.rawInput, actual)
			}

			// 检查异常提示信息是否符合预期
			if !strings.Contains(err.Error(), "清洗后的文件名为空") {
				t.Errorf("抛出的异常信息不准确，实际为: %v", err)
			} else {
				t.Logf("[OK] 成功捕获到预期的清洗异常熔断: %v", err)
			}
		})
	}
}
