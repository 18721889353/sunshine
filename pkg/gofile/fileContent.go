// Package gofile 是文件和目录管理工具库，提供路径处理、文件读写、目录遍历、字节搜索等操作。
package gofile

import (
	"bytes"
)

// FindSubBytes 查找第一个匹配的子串，返回包含起始标记和结束标记的内容
//
// 参数：
//
//	data  - 要搜索的字节数据
//	start - 起始标记
//	end   - 结束标记
//
// 返回值：
//
//	[]byte - 匹配到的子串（包含起始和结束标记），未匹配到则返回空切片
func FindSubBytes(data []byte, start []byte, end []byte) []byte {
	startIndex := bytes.Index(data, start)
	endIndex := bytes.Index(data, end)
	if startIndex >= endIndex {
		return []byte{}
	}
	if len(data) >= endIndex+len(end) {
		endIndex += len(end)
	}
	return data[startIndex:endIndex]
}

// FindAllSubBytes 查找所有匹配的子串，返回包含起始标记和结束标记的内容
//
// 参数：
//
//	data  - 要搜索的字节数据
//	start - 起始标记
//	end   - 结束标记
//
// 返回值：
//
//	[][]byte - 所有匹配到的子串切片
func FindAllSubBytes(data []byte, start []byte, end []byte) [][]byte {
	subBytes := [][]byte{}

	for {
		subString, endIndex := findSubByte2(data, start, end)
		if len(subString) == 0 {
			break
		}
		subBytes = append(subBytes, subString)
		data = data[endIndex:]
	}

	return subBytes
}

// findSubByte2 查找单个子串并返回结束位置，供 FindAllSubBytes 内部使用
//
// 参数：
//
//	data  - 要搜索的字节数据
//	start - 起始标记
//	end   - 结束标记
//
// 返回值：
//
//	[]byte - 匹配到的子串
//	int    - 匹配结束位置
func findSubByte2(data []byte, start []byte, end []byte) ([]byte, int) {
	startIndex := bytes.Index(data, start)
	endIndex := bytes.Index(data, end)
	if startIndex >= endIndex {
		return []byte{}, 0
	}
	if len(data) >= endIndex+len(end) {
		endIndex += len(end)
	}
	return data[startIndex:endIndex], endIndex
}

// FindSubBytesNotIn 查找第一个匹配的子串，不包含起始和结束标记
//
// 参数：
//
//	data  - 要搜索的字节数据
//	start - 起始标记
//	end   - 结束标记
//
// 返回值：
//
//	[]byte - 起始和结束标记之间的内容，未匹配到则返回空切片
func FindSubBytesNotIn(data []byte, start []byte, end []byte) []byte {
	startIndex := bytes.Index(data, start)
	endIndex := bytes.Index(data, end)
	if startIndex+len(start) >= endIndex {
		return []byte{}
	}
	return data[startIndex+len(start) : endIndex]
}
