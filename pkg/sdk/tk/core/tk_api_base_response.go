package core

// BaseTkAPIResponse 途刻API基础响应结构
type BaseTkAPIResponse struct {
	Code int64  `json:"code"`
	Msg  string `json:"msg"`
}
