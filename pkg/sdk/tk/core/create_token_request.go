package core

import (
	"encoding/json"
)

type CreateTokenRequest struct {
	BaseTkApiRequest
	param *CreateTokenParam
}

func (r *CreateTokenRequest) GetParamObject() interface{} {
	return r.param
}

func (r *CreateTokenRequest) GetParams() *CreateTokenParam {
	return r.param
}
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

func (r *CreateTokenRequest) GetUrlPath() string {
	return "/Api/Common/Auth/getToken"
}

func NewCreateTokenRequest() *CreateTokenRequest {
	request := &CreateTokenRequest{
		param: &CreateTokenParam{},
	}
	request.SetConfig(GetTkConfig())
	request.SetClient(DefaultTkApiClient)
	return request
}

type CreateTokenResponse struct {
	BaseTkApiResponse
	Data CreateTokenData `json:"data"`
}

type CreateTokenData struct {
	Token string `json:"token"`
}

type CreateTokenParam struct {
	AppId     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}
