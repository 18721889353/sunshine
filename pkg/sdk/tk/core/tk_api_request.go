package core

type TkAPIRequest interface {
	GetConfig() *TkConfig
	SetConfig(config *TkConfig)
	GetParamObject() interface{}
	GetURLPath() string
}

type BaseTkAPIRequest struct {
	config *TkConfig
	client *TkAPIClient
}

func (r *BaseTkAPIRequest) GetConfig() *TkConfig {
	return r.config
}

func (r *BaseTkAPIRequest) SetConfig(config *TkConfig) {
	r.config = config
}

func (r *BaseTkAPIRequest) GetClient() *TkAPIClient {
	return r.client
}

func (r *BaseTkAPIRequest) SetClient(client *TkAPIClient) {
	r.client = client
}
