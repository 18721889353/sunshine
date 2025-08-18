package get_card_request

import (
	"context"
	"encoding/json"
	"fmt"

	get_card_response "github.com/18721889353/sunshine/pkg/sdk/tk/api/get_card/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtranslations "github.com/go-playground/validator/v10/translations/zh"
)

type GetCardRequest struct {
	core.BaseTkApiRequest
	Param *GetCardParam
}

func (c *GetCardRequest) GetUrlPath() string {
	return "/Api/Order/Order/getCard"
}

func New() *GetCardRequest {
	request := &GetCardRequest{
		Param: &GetCardParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkApiClient)
	return request

}

func (c *GetCardRequest) Execute(accessToken string) (*get_card_response.GetCardResponse, error) {
	if err := c.Param.Validate(); err != nil {
		return nil, err
	}
	responseJson, err := c.GetClient().Request(c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &get_card_response.GetCardResponse{}
	err = json.Unmarshal([]byte(responseJson), response)
	if err != nil {
		return nil, err
	}
	return response, nil

}
func (c *GetCardRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*get_card_response.GetCardResponse, error) {
	if err := c.Param.Validate(); err != nil {
		return nil, err
	}
	responseJson, err := c.GetClient().RequestWithContext(ctx, c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &get_card_response.GetCardResponse{}
	err = json.Unmarshal([]byte(responseJson), response)
	if err != nil {
		return nil, err
	}
	return response, nil

}

func (c *GetCardRequest) GetParamObject() interface{} {
	return c.Param
}

func (c *GetCardRequest) GetParams() *GetCardParam {
	return c.Param
}

type GetCardParam struct {
	OrderSn string `json:"order_sn" validate:"required"`
}

func (c *GetCardParam) Validate() error {
	zhCh := zh.New()
	validate := validator.New()
	uni := ut.New(zhCh)
	trans, _ := uni.GetTranslator("zh")
	//验证器注册翻译器
	_ = zhtranslations.RegisterDefaultTranslations(validate, trans)

	err := validate.Struct(c)
	if err != nil {
		errs := err.(validator.ValidationErrors)
		var slice []string
		for _, e := range errs {
			slice = append(slice, e.Translate(trans))
		}
		return fmt.Errorf("%v", slice)
	}
	return nil
}
