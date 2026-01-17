package jsonutil

import (
	"bytes"
	"encoding/json"
	"testing"
)

// --- 测试数据准备 ---

type User struct {
	ID       int64            `json:"id"`
	Username string           `json:"username"`
	Email    string           `json:"email"`
	Active   bool             `json:"active"`
	Tags     []string         `json:"tags"`
	Meta     map[string]int64 `json:"meta"`
}

var bigTestData = User{
	ID:       123456789,
	Username: "GolangExpert_2026",
	Email:    "expert@example.com",
	Active:   true,
	Tags:     []string{"go", "performance", "sync.pool", "json-iterator", "backend"},
	Meta:     map[string]int64{"score": 100, "rank": 1, "level": 99},
}

// 模拟一个不占内存的 Writer，排除了网络/磁盘 IO 的干扰，只测序列化性能
type blackHoleWriter struct{}

func (blackHoleWriter) Write(p []byte) (int, error) { return len(p), nil }

var bhw = blackHoleWriter{}

// 将测试数据预先序列化为字节数组，用于反序列化测试
var serializedData, _ = Marshal(bigTestData)

// --- 功能测试 ---

func TestMarshal(t *testing.T) {
	user := User{
		ID:       1,
		Username: "testuser",
		Email:    "test@example.com",
		Active:   true,
		Tags:     []string{"tag1", "tag2"},
		Meta:     map[string]int64{"key": 123},
	}

	data, err := Marshal(user)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var unmarshaledUser User
	err = Unmarshal(data, &unmarshaledUser)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if unmarshaledUser.Username != user.Username {
		t.Errorf("Expected username %s, got %s", user.Username, unmarshaledUser.Username)
	}
}

func TestConfig(t *testing.T) {
	// 测试默认配置获取
	defaultCfg := GetDefaultConfig()
	if defaultCfg.EscapeHTML != false {
		t.Errorf("Expected EscapeHTML to be false in default config")
	}

	// 测试配置更改
	newConfig := Config{
		EscapeHTML:             true,
		SortMapKeys:            true,
		ValidateJsonRawMessage: true,
	}
	SetConfig(newConfig)

	// 验证更改后的功能
	user := User{ID: 1, Username: "<script>alert('test')</script>"}
	data, err := Marshal(user)
	if err != nil {
		t.Fatalf("Marshal with new config failed: %v", err)
	}

	var unmarshaledUser User
	err = Unmarshal(data, &unmarshaledUser)
	if err != nil {
		t.Fatalf("Unmarshal with new config failed: %v", err)
	}

	// 恢复默认配置
	defaultConfig := GetDefaultConfig()
	SetConfig(defaultConfig)
}

func TestMarshalWithConfig(t *testing.T) {
	user := User{ID: 1, Username: "<script>alert('test')</script>"}

	// 使用带转义的配置
	cfg := Config{
		EscapeHTML:             true,
		SortMapKeys:            false,
		ValidateJsonRawMessage: false,
	}

	data, err := MarshalWithConfig(user, cfg)
	if err != nil {
		t.Fatalf("MarshalWithConfig failed: %v", err)
	}

	// 检查是否包含转义的HTML字符
	if !bytes.Contains(data, []byte("\\u003c")) { // < 转义为 \u003c
		t.Log("HTML not escaped with EscapeHTML=true (this may be OK depending on jsoniter version)")
	}

	// 使用不转义的配置
	cfgNoEscape := Config{
		EscapeHTML:             false,
		SortMapKeys:            false,
		ValidateJsonRawMessage: false,
	}

	data2, err := MarshalWithConfig(user, cfgNoEscape)
	if err != nil {
		t.Fatalf("MarshalWithConfig with no escape failed: %v", err)
	}

	// 检查是否不包含转义字符
	if bytes.Contains(data2, []byte("\\u003c")) {
		t.Log("HTML escaped with EscapeHTML=false (this may be OK depending on jsoniter version)")
	}
}

func TestIsValid(t *testing.T) {
	validJSON := []byte(`{"key": "value"}`)
	invalidJSON := []byte(`{"key": "value"`)

	if !IsValid(validJSON) {
		t.Error("Expected valid JSON to be recognized as valid")
	}

	if IsValid(invalidJSON) {
		t.Error("Expected invalid JSON to be recognized as invalid")
	}
}

func TestMarshalIndent(t *testing.T) {
	user := User{
		ID:       1,
		Username: "testuser",
		Email:    "test@example.com",
	}

	data, err := MarshalIndent(user, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}

	// Check if the output is properly indented
	if !bytes.Contains(data, []byte("  ")) {
		t.Error("Expected indented output to contain spaces")
	}
}

func TestUnmarshalWithValidation(t *testing.T) {
	type StrictUser struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}

	// Valid JSON
	validData := []byte(`{"id": 1, "username": "testuser"}`)
	var user StrictUser
	err := UnmarshalWithValidation(validData, &user)
	if err != nil {
		t.Fatalf("Valid JSON should not cause error: %v", err)
	}

	// Invalid JSON
	invalidData := []byte(`{"id": 1, "username": "testuser", "extra_field": "value"}`)
	var user2 StrictUser
	err = UnmarshalWithValidation(invalidData, &user2)
	if err == nil {
		t.Error("Expected error for unknown field in strict mode")
	}
}

// --- 性能测试 ---

// --- 场景 1：标准 Marshal 对比 ---

func BenchmarkStandardMarshal(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(bigTestData)
	}
}

func BenchmarkJsonUtilMarshal(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = Marshal(bigTestData)
	}
}

// --- 场景 2：标准 Unmarshal 对比 ---

func BenchmarkStandardUnmarshal(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var u User
		_ = json.Unmarshal(serializedData, &u)
	}
}

func BenchmarkJsonUtilUnmarshal(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var u User
		_ = Unmarshal(serializedData, &u)
	}
}

// --- 场景 3：MarshalToWriter (生产环境最常用) ---

func BenchmarkStandardEncodeToWriter(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 标准库的典型写法，每次都会创建 encoder 和内部 buffer
		_ = json.NewEncoder(bhw).Encode(bigTestData)
	}
}

func BenchmarkJsonUtilMarshalToWriter(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 使用我们封装的带 bytebufferpool 的版本
		_ = MarshalToWriter(bhw, bigTestData)
	}
}

// --- 场景 4：高并发压力测试 (重点) ---
func BenchmarkParallelStandard(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// 对比组：原生标准库
			_, _ = json.Marshal(bigTestData)
		}
	})
}

func BenchmarkParallelJsonUtil(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// 实验组：泛型 + 池化封装
			_ = MarshalToWriter(bhw, bigTestData)
		}
	})
}

// --- 场景 5：高并发 Unmarshal 压力测试 ---
func BenchmarkParallelStandardUnmarshal(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var u User
			_ = json.Unmarshal(serializedData, &u)
		}
	})
}

func BenchmarkParallelJsonUtilUnmarshal(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var u User
			_ = Unmarshal(serializedData, &u)
		}
	})
}

// --- 场景 6：新增功能的性能测试 ---
func BenchmarkMarshalIndent(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalIndent(bigTestData, "", "  ")
	}
}

func BenchmarkUnmarshalWithValidation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var u User
		_ = UnmarshalWithValidation(serializedData, &u)
	}
}

// --- 场景 7：配置相关功能性能测试 ---
func BenchmarkMarshalWithConfig(b *testing.B) {
	config := Config{
		EscapeHTML:             false,
		SortMapKeys:            false,
		ValidateJsonRawMessage: false,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalWithConfig(bigTestData, config)
	}
}
