// Package gosm3 提供国密 SM3 哈希算法实现。
package gosm3

import (
	"encoding/hex"
	"github.com/tjfoc/gmsm/sm3"
)

// SM3Option 是用于配置SM3的函数类型
type SM3Option func(*sm3Options)

// sm3Options 包含SM3的所有可配置选项
type sm3Options struct {
	// 当前版本不包含配置选项，为将来扩展预留
}

// defaultSM3Options 返回默认的SM3选项
func defaultSM3Options() *sm3Options {
	return &sm3Options{}
}

// apply 应用所有提供的选项
func (o *sm3Options) apply(opts ...SM3Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// SM3 封装了SM3算法相关的操作
type SM3 struct {
	// 当前版本不包含配置选项，为将来扩展预留
}

// NewSM3 创建一个新的SM3实例
func NewSM3(opts ...SM3Option) *SM3 {
	o := defaultSM3Options()
	o.apply(opts...)
	return &SM3{}
}

// HashResult 包装哈希结果，支持链式调用转换格式
type HashResult struct {
	*Result
}

// Result 包装通用结果数据
type Result struct {
	data []byte
	err  error
}

// Hash 使用SM3算法对数据进行哈希计算
func (s *SM3) Hash(data []byte) *HashResult {
	hash := sm3.Sm3Sum(data)
	return &HashResult{&Result{data: hash[:], err: nil}}
}

// HashString 使用SM3算法对字符串进行哈希计算
func (s *SM3) HashString(data string) *HashResult {
	return s.Hash([]byte(data))
}

// ToBytes 返回原始字节数据
func (r *Result) ToBytes() ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.data, nil
}

// ToHex 将结果转换为十六进制字符串
func (r *Result) ToHex() (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return hex.EncodeToString(r.data), nil
}
