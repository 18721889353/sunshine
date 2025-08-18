package core

import (
	"github.com/18721889353/sunshine/pkg/sdk/tk/errors"
	"github.com/18721889353/sunshine/pkg/sdk/tk/utils"
)

type GetAccessTokenParam struct {
	Config    *TkConfig
	AppId     string
	AppSecret string
}

func GetAccessToken(param *GetAccessTokenParam) (string, error) {
	request := NewCreateTokenRequest()
	request.GetParams().AppId = param.AppId
	request.GetParams().AppSecret = param.AppSecret

	if param.Config != nil {
		request.SetConfig(param.Config)
	}

	response, err := request.Execute("")
	if err != nil {
		return "", err
	}

	if response.Code == 0 && response.Data.Token != "" {
		return response.Data.Token, nil
	} else {
		return "", errors.NewTkErrorWithMessage(errors.GetTokenError, utils.MarshalNoErr(response))
	}
}
