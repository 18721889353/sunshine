package core

type TkApiRequest interface {
	GetConfig() *TkConfig
	SetConfig(config *TkConfig)
	GetParamObject() interface{}
	GetUrlPath() string
}

type BaseTkApiRequest struct {
	config *TkConfig
	client *TkApiClient
}

func (r *BaseTkApiRequest) GetConfig() *TkConfig {
	return r.config
}

func (r *BaseTkApiRequest) SetConfig(config *TkConfig) {
	r.config = config
}

func (r *BaseTkApiRequest) GetClient() *TkApiClient {
	return r.client
}

func (r *BaseTkApiRequest) SetClient(client *TkApiClient) {
	r.client = client
}
