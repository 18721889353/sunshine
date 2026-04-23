// Package decimalutil provides utility functions for decimal arithmetic operations.
package decimalutil

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// OperationResult 表示运算结果
type OperationResult struct {
	Value decimal.Decimal
	Err   error
}

// Add 执行加法，支持 string/int/int64/float64 类型，并可指定精度
// 精度参数必须为非负整数，否则返回错误
func Add(a, b interface{}, precision ...int32) OperationResult {
	decimalA, err := parseDecimal(a)
	if err != nil {
		return OperationResult{Err: fmt.Errorf("解析 a: %w", err)}
	}

	decimalB, err := parseDecimal(b)
	if err != nil {
		return OperationResult{Err: fmt.Errorf("解析 b: %w", err)}
	}

	// 默认保留 2 位小数，也可由调用方指定
	precisionVal := int32(2)
	if len(precision) > 0 {
		if precision[0] < 0 {
			return OperationResult{Err: fmt.Errorf("精度不能为负数: %d", precision[0])}
		}
		precisionVal = precision[0]
	}

	result := decimalA.Add(decimalB).Round(precisionVal)
	return OperationResult{Value: result}
}

// Subtract 执行减法，支持 string/int/int64/float64 类型，并可指定精度
// 精度参数必须为非负整数，否则返回错误
func Subtract(a, b interface{}, precision ...int32) OperationResult {
	decimalA, err := parseDecimal(a)
	if err != nil {
		return OperationResult{Err: fmt.Errorf("解析 a: %w", err)}
	}

	decimalB, err := parseDecimal(b)
	if err != nil {
		return OperationResult{Err: fmt.Errorf("解析 b: %w", err)}
	}

	// 默认保留 2 位小数，也可由调用方指定
	precisionVal := int32(2)
	if len(precision) > 0 {
		if precision[0] < 0 {
			return OperationResult{Err: fmt.Errorf("精度不能为负数: %d", precision[0])}
		}
		precisionVal = precision[0]
	}

	result := decimalA.Sub(decimalB).Round(precisionVal)
	return OperationResult{Value: result}
}

// Multiply 执行乘法，支持 string/int/int64/float64 类型，并可指定精度
// 精度参数必须为非负整数，否则返回错误
func Multiply(a, b interface{}, precision ...int32) OperationResult {
	decimalA, err := parseDecimal(a)
	if err != nil {
		return OperationResult{Err: fmt.Errorf("解析 a: %w", err)}
	}

	decimalB, err := parseDecimal(b)
	if err != nil {
		return OperationResult{Err: fmt.Errorf("解析 b: %w", err)}
	}

	// 默认保留 2 位小数，也可由调用方指定
	precisionVal := int32(2)
	if len(precision) > 0 {
		if precision[0] < 0 {
			return OperationResult{Err: fmt.Errorf("精度不能为负数: %d", precision[0])}
		}
		precisionVal = precision[0]
	}

	result := decimalA.Mul(decimalB).Round(precisionVal)
	return OperationResult{Value: result}
}

// Divide 执行除法，支持精度设置
// 精度参数必须为非负整数，否则返回错误
func Divide(a, b interface{}, precision int32) OperationResult {
	if precision < 0 {
		return OperationResult{Err: fmt.Errorf("精度不能为负数: %d", precision)}
	}

	decimalA, err := parseDecimal(a)
	if err != nil {
		return OperationResult{Err: fmt.Errorf("解析 a: %w", err)}
	}

	decimalB, err := parseDecimal(b)
	if err != nil {
		return OperationResult{Err: fmt.Errorf("解析 b: %w", err)}
	}

	if decimalB.IsZero() {
		return OperationResult{Err: fmt.Errorf("除数不能为零")}
	}

	return OperationResult{Value: decimalA.DivRound(decimalB, precision)}
}

// parseDecimal 将任意类型解析为 decimal.Decimal
func parseDecimal(v interface{}) (decimal.Decimal, error) {
	switch val := v.(type) {
	case string:
		if val == "" {
			return decimal.Zero, fmt.Errorf("输入为空")
		}
		d, err := decimal.NewFromString(val)
		if err != nil {
			return decimal.Zero, fmt.Errorf("解析字符串失败: %w", err)
		}
		return d, nil
	case int:
		return decimal.NewFromInt(int64(val)), nil
	case int64:
		return decimal.NewFromInt(val), nil
	case float64:
		return decimal.NewFromFloat(val), nil
	default:
		return decimal.Zero, fmt.Errorf("不支持的类型 %T", v)
	}
}

// FormatDecimal 统一格式化输出
func FormatDecimal(d decimal.Decimal, precision int32) string {
	return d.Round(precision).StringFixed(precision)
}
