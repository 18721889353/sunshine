package utils

import (
	"strconv"
	"strings"
)

// MaxStringID is the maximum string ID
const MaxStringID = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"

// StrToInt string to int
// Note: error is intentionally ignored for simplicity, use StrToIntE if you need error handling
func StrToInt(str string) int {
	v, _ := strconv.Atoi(str) //nolint:errcheck
	return v
}

// StrToIntE string to int with error
func StrToIntE(str string) (int, error) {
	return strconv.Atoi(str)
}

// StrToUint32 string to uint32
// Note: error is intentionally ignored for simplicity, use StrToUint32E if you need error handling
func StrToUint32(str string) uint32 {
	v, _ := strconv.ParseUint(str, 10, 64) //nolint:errcheck
	return uint32(v)
}

// StrToUint32E string to uint32 with error
func StrToUint32E(str string) (uint32, error) {
	v, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return 0, err
	}

	return uint32(v), nil
}

// StrToUint64 string to uint64
// Note: error is intentionally ignored for simplicity, use StrToUint64E if you need error handling
func StrToUint64(str string) uint64 {
	v, _ := strconv.ParseUint(str, 10, 64) //nolint:errcheck
	return v
}

// StrToUint64E string to uint64 with error
func StrToUint64E(str string) (uint64, error) {
	return strconv.ParseUint(str, 10, 64)
}

// StrToFloat32 string to float32
// Note: error is intentionally ignored for simplicity, use StrToFloat32E if you need error handling
func StrToFloat32(str string) float32 {
	v, _ := strconv.ParseFloat(str, 32) //nolint:errcheck
	return float32(v)
}

// StrToFloat32E string to float32 with error
func StrToFloat32E(str string) (float32, error) {
	v, err := strconv.ParseFloat(str, 32)
	if err != nil {
		return 0, err
	}
	return float32(v), nil
}

// StrToFloat64 string to float64
// Note: error is intentionally ignored for simplicity, use StrToFloat64E if you need error handling
func StrToFloat64(str string) float64 {
	v, _ := strconv.ParseFloat(str, 64) //nolint:errcheck
	return v
}

// StrToFloat64E string to float64 with error
func StrToFloat64E(str string) (float64, error) {
	return strconv.ParseFloat(str, 64)
}

// IntToStr int to string
func IntToStr(v int) string {
	return strconv.Itoa(v)
}

// Uint64ToStr uint64 to string
func Uint64ToStr(v uint64) string {
	return strconv.FormatUint(v, 10)
}

// Int64ToStr int64 to string
func Int64ToStr(v int64) string {
	return strconv.FormatInt(v, 10)
}

// ProtoInt32ToInt convert proto int32 to int
func ProtoInt32ToInt(v int32) int {
	return int(v)
}

// IntToProtoInt32 convert int to proto int32
func IntToProtoInt32(v int) int32 {
	return int32(v)
}

// ProtoInt64ToUint64 convert proto int64 to uint64
func ProtoInt64ToUint64(v int64) uint64 {
	return uint64(v)
}

// Uint64ToProtoInt64 convert uint64 to proto int64
func Uint64ToProtoInt64(v uint64) int64 {
	return int64(v)
}

// Uint64SliceToStringSlice 将 []Uint64 转换为 []string
func Uint64SliceToStringSlice(slice []uint64) string {
	result := make([]string, len(slice))
	for i, v := range slice {
		result[i] = strconv.FormatUint(v, 10)
	}
	return strings.Join(result, ",")
}

// UniqueStringSlice 将 字符切片去重
func UniqueStringSlice(slice []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, v := range slice {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// HasDuplicateStringSlice 判断字符切片是否有重复
func HasDuplicateStringSlice(slice []string) bool {
	seen := make(map[string]struct{})
	for _, v := range slice {
		if _, ok := seen[v]; ok {
			return true
		}
		seen[v] = struct{}{}
	}
	return false
}

// UniqueUint64Slice 将 uint64 切片去重
func UniqueUint64Slice(slice []uint64) []uint64 {
	seen := make(map[uint64]struct{})
	result := make([]uint64, 0, len(slice))
	for _, v := range slice {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// HasDuplicateUint64Slice 判断 uint64 切片是否有重复
func HasDuplicateUint64Slice(slice []uint64) bool {
	seen := make(map[uint64]struct{})
	for _, v := range slice {
		if _, ok := seen[v]; ok {
			return true
		}
		seen[v] = struct{}{}
	}
	return false
}
