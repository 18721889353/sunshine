package core

// TkAPIRequest 途刻API请求接口
type TkAPIRequest interface {
	GetConfig() *TkConfig
	SetConfig(config *TkConfig)
	GetParamObject() interface{}
	GetURLPath() string
}

// BaseTkAPIRequest 途刻API基础请求结构
type BaseTkAPIRequest struct {
	config *TkConfig
	client *TkAPIClient
}

// GetConfig 获取配置
func (r *BaseTkAPIRequest) GetConfig() *TkConfig {
	return r.config
}

// SetConfig 设置配置
func (r *BaseTkAPIRequest) SetConfig(config *TkConfig) {
	r.config = config
}

// GetClient 获取客户端
func (r *BaseTkAPIRequest) GetClient() *TkAPIClient {
	return r.client
}

// SetClient 设置客户端
func (r *BaseTkAPIRequest) SetClient(client *TkAPIClient) {
	r.client = client
}
