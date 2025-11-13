package create_token_request

import (
	"context"
	"encoding/json"
	create_token_response "github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"
	"github.com/18721889353/sunshine/pkg/sdk/tk/errors"
	"github.com/18721889353/sunshine/pkg/sdk/tk/utils"
)

type CreateTokenRequest struct {
	core.BaseTkApiRequest
	param *CreateTokenParam
}

func (r *CreateTokenRequest) GetParamObject() interface{} {
	return r.param
}

func (r *CreateTokenRequest) GetParams() *CreateTokenParam {
	return r.param
}

func (r *CreateTokenRequest) Execute(accessToken string) (*create_token_response.CreateTokenResponse, error) {
	responseJson, err := r.GetClient().Request(r, accessToken)
	if err != nil {
		return nil, err
	}
	resp := &create_token_response.CreateTokenResponse{}
	err = json.Unmarshal([]byte(responseJson), resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (r *CreateTokenRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*create_token_response.CreateTokenResponse, error) {
	responseJson, err := r.GetClient().RequestWithContext(ctx, r, accessToken)
	if err != nil {
		return nil, err
	}
	resp := &create_token_response.CreateTokenResponse{}
	err = json.Unmarshal([]byte(responseJson), resp)
	if err != nil {
		return nil, err
	}
	return resp, nil

}

func (r *CreateTokenRequest) GetUrlPath() string {
	return "/Api/Common/Auth/getToken"
}

func NewCreateTokenRequest() *CreateTokenRequest {
	request := &CreateTokenRequest{
		param: &CreateTokenParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkApiClient)
	return request
}

type CreateTokenParam struct {
	AppId     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

type GetAccessTokenParam struct {
	Config    *core.TkConfig
	AppId     string
	AppSecret string
}

func GetAccessToken(param *GetAccessTokenParam) (string, error) {
	req := NewCreateTokenRequest()
	req.GetParams().AppId = param.AppId
	req.GetParams().AppSecret = param.AppSecret

	if param.Config != nil {
		req.SetConfig(param.Config)
	}

	resp, err := req.Execute("")
	if err != nil {
		return "", err
	}

	if resp.Code == 0 && resp.Data.Token != "" {
		return resp.Data.Token, nil
	} else {
		return "", errors.NewTkErrorWithMessage(errors.GetTokenError, utils.MarshalNoErr(resp))
	}
}

func GetAccessTokenWithContext(ctx context.Context, param *GetAccessTokenParam) (string, error) {
	req := NewCreateTokenRequest()
	req.GetParams().AppId = param.AppId
	req.GetParams().AppSecret = param.AppSecret

	if param.Config != nil {
		req.SetConfig(param.Config)
	}

	resp, err := req.ExecuteWithContext(ctx, "")
	if err != nil {
		return "", err
	}

	if resp.Code == 0 && resp.Data.Token != "" {
		return resp.Data.Token, nil
	} else {
		return "", errors.NewTkErrorWithMessage(errors.GetTokenError, utils.MarshalNoErr(resp))
	}
}
