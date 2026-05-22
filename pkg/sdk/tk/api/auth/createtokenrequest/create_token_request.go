// Package createtokenrequest 提供创建令牌API的请求类型
package createtokenrequest

import (
	"context"
	"encoding/json"

	createtokenresponse "github.com/18721889353/sunshine/pkg/sdk/tk/api/auth/createtokenresponse"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"
	"github.com/18721889353/sunshine/pkg/sdk/tk/tkerrors"
	"github.com/18721889353/sunshine/pkg/sdk/tk/utils"
)

// CreateTokenRequest 创建令牌请求
type CreateTokenRequest struct {
	core.BaseTkAPIRequest
	param *CreateTokenParam
}

// GetParamObject 获取参数对象
func (r *CreateTokenRequest) GetParamObject() interface{} {
	return r.param
}

// GetParams 获取参数
func (r *CreateTokenRequest) GetParams() *CreateTokenParam {
	return r.param
}

// ExecuteWithContext 带上下文执行创建令牌请求
func (r *CreateTokenRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*createtokenresponse.CreateTokenResponse, error) {
	responseJSON, err := r.GetClient().RequestWithContext(ctx, r, accessToken)
	if err != nil {
		return nil, err
	}
	resp := &createtokenresponse.CreateTokenResponse{}
	err = json.Unmarshal([]byte(responseJSON), resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// GetURLPath 获取URL路径
func (r *CreateTokenRequest) GetURLPath() string {
	return "/Api/Common/Auth/getToken"
}

// NewCreateTokenRequest 创建新的令牌请求实例
func NewCreateTokenRequest() *CreateTokenRequest {
	request := &CreateTokenRequest{
		param: &CreateTokenParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkAPIClient)
	return request
}

// CreateTokenParam 创建令牌参数
type CreateTokenParam struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

// GetAccessTokenParam 获取访问令牌参数
type GetAccessTokenParam struct {
	Config    *core.TkConfig
	AppID     string
	AppSecret string
}

// GetAccessTokenWithContext 带上下文获取访问令牌
func GetAccessTokenWithContext(ctx context.Context, param *GetAccessTokenParam) (string, error) {
	req := NewCreateTokenRequest()
	req.GetParams().AppID = param.AppID
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
	}
	return "", tkerrors.NewTkErrorWithMessage(tkerrors.GetTokenError, utils.MarshalNoErr(resp))
}
