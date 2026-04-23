// Package utils 提供通用工具函数
package utils

import "encoding/json"

func MarshalNoErr(object interface{}) string {
	bs, _ := json.Marshal(object)
	return string(bs)
}
