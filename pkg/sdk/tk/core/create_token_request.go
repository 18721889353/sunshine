package core

import (
	"encoding/json"
)

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
type CreateTokenRequest struct {
	BaseTkAPIRequest
	param *CreateTokenParam
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
func (r *CreateTokenRequest) GetParamObject() interface{} {
	return r.param
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
func (r *CreateTokenRequest) GetParams() *CreateTokenParam {
	return r.param
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
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

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
func (r *CreateTokenRequest) GetURLPath() string {
	return "/Api/Common/Auth/getToken"
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
func NewCreateTokenRequest() *CreateTokenRequest {
	request := &CreateTokenRequest{
		param: &CreateTokenParam{},
	}
	request.SetConfig(GetTkConfig())
	request.SetClient(DefaultTkAPIClient)
	return request
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
type CreateTokenResponse struct {
	BaseTkAPIResponse
	Data CreateTokenData `json:"data"`
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
type CreateTokenData struct {
	Token string `json:"token"`
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
type CreateTokenParam struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}
