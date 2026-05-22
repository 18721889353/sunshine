// Package core 提供途刻SDK核心功能，包括HTTP客户端、API客户端和配置管理。
package core

// BaseTkAPIResponse 途刻API基础响应结构
type BaseTkAPIResponse struct {
	Code int64  `json:"code"`
	Msg  string `json:"msg"`
}
