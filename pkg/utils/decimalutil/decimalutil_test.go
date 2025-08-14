package decimalutil

import (
	"strings"
	"testing"
)

// ----------------------------
// 公共测试数据结构
// ----------------------------
type testAddCase struct {
	name      string
	a         interface{}
	b         interface{}
	precision int32
	expected  string
	errMsg    string
}

// ----------------------------
// Add 函数测试
// ----------------------------
func TestAdd(t *testing.T) {
	tests := []testAddCase{
		// 有效输入
		{"Integers", 1, 2, 2, "3.00", ""},
		{"Floats", 1.5, 2.5, 2, "4.00", ""},
		{"Strings", "1.1", "2.2", 2, "3.30", ""},
		{"MixedTypes", 3, "2.5", 1, "5.5", ""},
		{"HighPrecision", "3.1415", "2.7182", 4, "5.8597", ""},
		{"NegativeNumbers", "-5", "3", 0, "-2", ""},
		{"Zero", "0", "0", 2, "0.00", ""},

		// 无效输入
		{"InvalidStringA", "a1", "2", 2, "", "解析 a: 解析字符串失败"},
		{"InvalidStringB", "3", "0.1a", 2, "", "解析 b: 解析字符串失败"},
		{"EmptyString", "", "1", 2, "", "解析 a: 输入为空"},
		{"UnsupportedType", []int{1}, 2, 2, "", "解析 a: 不支持的类型"},
		{"NilValue", nil, "2", 2, "", "解析 a: 不支持的类型"},

		// 精度控制
		{"PrecisionZero", "3.1415", "2.7182", 0, "6", ""},
		{"NegativePrecision", "3.1415", "2.7182", -1, "", "精度不能为负数"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Add(tt.a, tt.b, tt.precision)
			if tt.errMsg != "" {
				if result.Err == nil {
					t.Fatal("Expected error but got none")
				}
				if !strings.Contains(result.Err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errMsg, result.Err)
				}
				return
			}

			if result.Err != nil {
				t.Fatalf("Unexpected error: %v", result.Err)
			}

			if result.Value.StringFixed(tt.precision) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result.Value.StringFixed(tt.precision))
			}
		})
	}
}

// ----------------------------
// Subtract 函数测试
// ----------------------------
func TestSubtract(t *testing.T) {
	tests := []testAddCase{
		{"Integers", 5, 3, 2, "2.00", ""},
		{"Floats", 5.5, 2.5, 2, "3.00", ""},
		{"Strings", "5.5", "2.5", 2, "3.00", ""},
		{"NegativeResult", "2.5", "5.5", 2, "-3.00", ""},
		{"NegativePrecision", "3.1415", "2.7182", -1, "", "精度不能为负数"},
		{"InvalidString", "a1", "2", 2, "", "解析 a: 解析字符串失败"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Subtract(tt.a, tt.b, tt.precision)
			if tt.errMsg != "" {
				if result.Err == nil {
					t.Fatal("Expected error but got none")
				}
				if !strings.Contains(result.Err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errMsg, result.Err)
				}
				return
			}

			if result.Err != nil {
				t.Fatalf("Unexpected error: %v", result.Err)
			}

			if result.Value.StringFixed(tt.precision) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result.Value.StringFixed(tt.precision))
			}
		})
	}
}

// ----------------------------
// Multiply 函数测试
// ----------------------------
func TestMultiply(t *testing.T) {
	tests := []testAddCase{
		{"Integers", 3, 2, 2, "6.00", ""},
		{"Floats", 3.1415, 2, 2, "6.28", ""},
		{"Strings", "3.14", "2.71", 2, "8.51", ""},
		{"Negative", "-3", "2", 2, "-6.00", ""},
		{"NegativePrecision", "3.1415", "2.7182", -1, "", "精度不能为负数"},
		{"InvalidString", "a1", "2", 2, "", "解析 a: 解析字符串失败"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Multiply(tt.a, tt.b, tt.precision)
			if tt.errMsg != "" {
				if result.Err == nil {
					t.Fatal("Expected error but got none")
				}
				if !strings.Contains(result.Err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errMsg, result.Err)
				}
				return
			}

			if result.Err != nil {
				t.Fatalf("Unexpected error: %v", result.Err)
			}

			if result.Value.StringFixed(tt.precision) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result.Value.StringFixed(tt.precision))
			}
		})
	}
}

// ----------------------------
// Divide 函数测试
// ----------------------------
func TestDivide(t *testing.T) {
	tests := []testAddCase{
		{"Integers", 6, 2, 2, "3.00", ""},
		{"Floats", 5.5, 2.5, 2, "2.20", ""},
		{"Strings", "10", "3", 2, "3.33", ""},
		{"ZeroDenominator", "5", "0", 2, "", "除数不能为零"},
		{"NegativePrecision", "3.1415", "2.7182", -1, "", "精度不能为负数"},
		{"InvalidString", "a1", "2", 2, "", "解析 a: 解析字符串失败"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Divide(tt.a, tt.b, tt.precision)
			if tt.errMsg != "" {
				if result.Err == nil {
					t.Fatal("Expected error but got none")
				}
				if !strings.Contains(result.Err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errMsg, result.Err)
				}
				return
			}

			if result.Err != nil {
				t.Fatalf("Unexpected error: %v", result.Err)
			}

			if result.Value.StringFixed(tt.precision) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result.Value.StringFixed(tt.precision))
			}
		})
	}
}
