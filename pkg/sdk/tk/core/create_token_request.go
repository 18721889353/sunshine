package core

import (
	"encoding/json"
)

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
type CreateTokenRequest struct {
	BaseTkApiRequest
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
	responseJson, err := r.GetClient().Request(r, accessToken)
	if err != nil {
		return nil, err
	}
	response := &CreateTokenResponse{}
	err = json.Unmarshal([]byte(responseJson), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
func (r *CreateTokenRequest) GetUrlPath() string {
	return "/Api/Common/Auth/getToken"
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
func NewCreateTokenRequest() *CreateTokenRequest {
	request := &CreateTokenRequest{
		param: &CreateTokenParam{},
	}
	request.SetConfig(GetTkConfig())
	request.SetClient(DefaultTkApiClient)
	return request
}

// GetAccessTokenParam 已废弃，使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
// Deprecated: 使用 github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/request 包中的 GetAccessTokenParam
type CreateTokenResponse struct {
	BaseTkApiResponse
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
	AppId     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}
