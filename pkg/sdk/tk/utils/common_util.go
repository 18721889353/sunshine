// Package utils 提供通用工具函数
package utils

import (
	"context"
	"encoding/json"

	"github.com/18721889353/sunshine/pkg/logger"
)

// MarshalNoErr 将对象序列化为JSON字符串，出错时返回空字符串
func MarshalNoErr(object interface{}) string {
	bs, err := json.Marshal(object)
	if err != nil {
		logger.WarnWithCtx(context.TODO(), "序列化对象为JSON失败", logger.Err(err))
		return ""
	}
	return string(bs)
}
