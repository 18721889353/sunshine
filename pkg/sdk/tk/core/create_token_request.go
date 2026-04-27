package core

import (
	"encoding/json"
)

// CreateTokenRequest 创建令牌请求（已废弃）
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 CreateTokenRequest
type CreateTokenRequest struct {
	BaseTkAPIRequest
	param *CreateTokenParam
}

// GetParamObject 获取参数对象（已废弃）
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的对应方法
func (r *CreateTokenRequest) GetParamObject() interface{} {
	return r.param
}

// GetParams 获取参数（已废弃）
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的对应方法
func (r *CreateTokenRequest) GetParams() *CreateTokenParam {
	return r.param
}

// Execute 执行创建令牌请求（已废弃）
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的对应方法
func (r *CreateTokenRequest) Execute(accessToken string) (*CreateTokenResponse, error) {
	responseJSON, err := r.GetClient().Request(r, accessToken)
	if err != nil {
		return nil, err
	}
	response := &CreateTokenResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// GetURLPath 获取URL路径（已废弃）
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的对应方法
func (r *CreateTokenRequest) GetURLPath() string {
	return "/Api/Common/Auth/getToken"
}

// NewCreateTokenRequest 创建新的令牌请求实例（已废弃）
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 NewCreateTokenRequest
func NewCreateTokenRequest() *CreateTokenRequest {
	request := &CreateTokenRequest{
		param: &CreateTokenParam{},
	}
	request.SetConfig(GetTkConfig())
	request.SetClient(DefaultTkAPIClient)
	return request
}

// CreateTokenResponse 创建令牌响应（已废弃）
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/response 包中的 CreateTokenResponse
type CreateTokenResponse struct {
	BaseTkAPIResponse
	Data CreateTokenData `json:"data"`
}

// CreateTokenData 创建令牌数据（已废弃）
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/response 包中的 CreateTokenData
type CreateTokenData struct {
	Token string `json:"token"`
}

// CreateTokenParam 创建令牌参数（已废弃）
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 CreateTokenParam
type CreateTokenParam struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}
