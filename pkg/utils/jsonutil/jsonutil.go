package jsonutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	jsoniter "github.com/json-iterator/go"
)

// Config 定义JSON配置选项
type Config struct {
	EscapeHTML             bool
	SortMapKeys            bool
	ValidateJsonRawMessage bool
}

// 默认配置
var defaultConfig = Config{
	EscapeHTML:             false, // 不转义HTML，提高性能
	SortMapKeys:            false, // 不对map key排序，提高性能
	ValidateJsonRawMessage: false, // 不自动验证JSON Raw Message，提高性能
}

// 全局API实例
var jsonAPI = createAPI(defaultConfig)

// 配置到API实例的缓存，避免重复创建
var (
	configCache = make(map[uint64]jsoniter.API)
	cacheMutex  = sync.RWMutex{}
)

// configToKey 将配置转换为唯一键值
func configToKey(cfg Config) uint64 {
	var key uint64
	if cfg.EscapeHTML {
		key |= 1
	}
	if cfg.SortMapKeys {
		key |= 2
	}
	if cfg.ValidateJsonRawMessage {
		key |= 4
	}
	return key
}

// createAPI 根据配置创建API实例
func createAPI(cfg Config) jsoniter.API {
	return jsoniter.Config{
		EscapeHTML:             cfg.EscapeHTML,
		SortMapKeys:            cfg.SortMapKeys,
		ValidateJsonRawMessage: cfg.ValidateJsonRawMessage,
	}.Froze()
}

// getCachedAPI 获取缓存的API实例，如果不存在则创建
func getCachedAPI(cfg Config) jsoniter.API {
	key := configToKey(cfg)

	cacheMutex.RLock()
	if api, exists := configCache[key]; exists {
		cacheMutex.RUnlock()
		return api
	}
	cacheMutex.RUnlock()

	// 双重检查锁定
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if api, exists := configCache[key]; exists {
		return api
	}

	api := createAPI(cfg)
	configCache[key] = api
	return api
}

// SetConfig 设置全局配置
func SetConfig(cfg Config) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	jsonAPI = createAPI(cfg)

	// 同时缓存新配置
	key := configToKey(cfg)
	configCache[key] = jsonAPI
}

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() Config {
	return defaultConfig
}

// MarshalToWriter 使用泛型 [T any] 消除 interface{} 导致的逃逸分配
func MarshalToWriter[T any](w io.Writer, v T) error {
	enc := jsonAPI.NewEncoder(w)
	return enc.Encode(v)
}

// Marshal 同样利用泛型优化内存路径
func Marshal[T any](v T) ([]byte, error) {
	return jsonAPI.Marshal(v)
}

// Unmarshal 使用泛型优化
func Unmarshal[T any](data []byte, v *T) error {
	return jsonAPI.Unmarshal(data, v)
}

// 兼容标准库的函数
func MarshalStd(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func UnmarshalStd(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// UnmarshalWithValidation 带验证的反序列化函数
func UnmarshalWithValidation[T any](data []byte, v *T) error {
	// 先验证JSON格式
	if !jsoniter.Valid(data) {
		return fmt.Errorf("invalid JSON")
	}

	decoder := jsonAPI.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields() // 不允许未知字段

	return decoder.Decode(v)
}

// MarshalIndent 带缩进的序列化
func MarshalIndent[T any](v T, prefix, indent string) ([]byte, error) {
	data, err := jsonAPI.Marshal(v)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = json.Indent(&buf, data, prefix, indent)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// IsValid 检查JSON字符串是否有效
func IsValid(data []byte) bool {
	return jsoniter.Valid(data)
}

// GetDecoder 获取配置好的解码器
func GetDecoder(r io.Reader) *jsoniter.Decoder {
	return jsonAPI.NewDecoder(r)
}

// GetEncoder 获取配置好的编码器
func GetEncoder(w io.Writer) *jsoniter.Encoder {
	return jsonAPI.NewEncoder(w)
}

// NewAPIWithConfig 创建一个新的API实例，使用指定配置
func NewAPIWithConfig(cfg Config) jsoniter.API {
	return getCachedAPI(cfg)
}

// MarshalWithConfig 使用指定配置进行序列化
func MarshalWithConfig[T any](v T, cfg Config) ([]byte, error) {
	api := getCachedAPI(cfg)
	return api.Marshal(v)
}
