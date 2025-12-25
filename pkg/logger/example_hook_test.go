package logger

import (
	"fmt"
	"testing"

	"go.uber.org/zap/zapcore"
)

// ExampleHook 展示如何创建一个可以访问字段数据的钩子函数
func standardHook() {
	// 创建一个标准的 zap 钩子函数，它不能直接访问字段数据
	standardHook := func(entry zapcore.Entry) error {
		fmt.Printf("Standard Hook - Level: %s, Message: %s\n", entry.Level, entry.Message)
		return nil
	}

	// 创建一个自定义钩子函数，它可以访问完整的字段数据
	customHook := func(entry zapcore.Entry, fields []Field) error {
		fmt.Printf("Custom Hook - Level: %s, Message: %s, Fields Count: %d\n", entry.Level, entry.Message, len(fields))
		// 打印所有字段的键和值
		for _, field := range fields {
			fmt.Printf("  Field - Key: %s, Value: %v\n", field.Key, getFieldValue(field))
		}
		return nil
	}

	// 初始化日志记录器并添加两种钩子
	Init(
		WithLevel("debug"),
		WithHooks(standardHook),     // 标准钩子
		WithCustomHooks(customHook), // 自定义钩子
	)

	// 记录一条带字段的日志
	Info("user login", Any("aaa",
		map[string]any{
			"username": "johndoe",
		}),
		String("user_id", "12345"), Int("attempts", 3))

}

// AdvancedHook 展示一个更高级的自定义钩子函数
func AdvancedHook() {
	customHook := func(entry zapcore.Entry, fields []Field) error {
		fmt.Printf("Level: %s\nMessage: %s\n", entry.Level, entry.Message)
		// 分析字段数据并打印键值对
		for _, field := range fields {
			key := field.Key
			value := getFieldValue(field)
			fmt.Printf("Field - %s: %v (Type: %v)\n", key, value, field.Type)
		}
		return nil
	}

	Init(
		WithLevel("debug"),
		WithCustomHooks(customHook),
	)
	// 记录不同类型的日志
	//Info("processing user data",
	//	String("user_id", "12345"),
	//	Int("age", 25),
	//	Any("metadata", map[string]interface{}{"role": "admin", "active": true}),
	//)

	Info("database connection failed",
		Err(fmt.Errorf("connection timeout")),
		String("host", "localhost"),
		Int("port", 5432),
	)
}

// getFieldValue extracts the value from a zap Field based on its type
func getFieldValue(field Field) interface{} {
	switch field.Type {
	case zapcore.StringType:
		return field.String
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
		return field.Integer
	case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
		return field.Integer
	case zapcore.BoolType:
		if b, ok := field.Interface.(bool); ok {
			return b
		}
		return false
	case zapcore.Float64Type:
		if f, ok := field.Interface.(float64); ok {
			return f
		}
		return 0.0
	case zapcore.ErrorType:
		if err, ok := field.Interface.(error); ok {
			return err.Error()
		}
		return ""
	case zapcore.ByteStringType:
		if b, ok := field.Interface.([]byte); ok {
			return string(b)
		}
		return field.Interface
	default:
		// 尝试处理可能的字节切片类型
		if b, ok := field.Interface.([]byte); ok {
			return string(b)
		}
		return field.Interface
	}
}
func TestHookExample(t *testing.T) {
	standardHook()
	//AdvancedHook()
}
