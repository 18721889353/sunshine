package create_order_request

import (
	"context"
	"encoding/json"
	"fmt"

	create_order_response "github.com/18721889353/sunshine/pkg/sdk/tk/api/create_order/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtranslations "github.com/go-playground/validator/v10/translations/zh"
)

type CreateOrderRequest struct {
	core.BaseTkAPIRequest
	Param *CreateOrderParam
}

func (c *CreateOrderRequest) GetURLPath() string {
	return "/Api/Order/Order/create"
}

func New() *CreateOrderRequest {
	request := &CreateOrderRequest{
		Param: &CreateOrderParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkAPIClient)
	return request

}

func (c *CreateOrderRequest) Execute(accessToken string) (*create_order_response.CreateOrderResponse, error) {
	if err := c.Param.Validate(); err != nil {
		return nil, err
	}
	responseJson, err := c.GetClient().Request(c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &create_order_response.CreateOrderResponse{}
	err = json.Unmarshal([]byte(responseJson), response)
	if err != nil {
		return nil, err
	}
	return response, nil

}

func (c *CreateOrderRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*create_order_response.CreateOrderResponse, error) {
	if err := c.Param.Validate(); err != nil {
		return nil, err
	}
	responseJson, err := c.GetClient().RequestWithContext(ctx, c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &create_order_response.CreateOrderResponse{}
	err = json.Unmarshal([]byte(responseJson), response)
	if err != nil {
		return nil, err
	}
	return response, nil

}

func (c *CreateOrderRequest) GetParamObject() interface{} {
	return c.Param
}

func (c *CreateOrderRequest) GetParams() *CreateOrderParam {
	return c.Param
}

type CreateOrderParam struct {
	CouponID   int    `json:"coupon_id,omitempty" validate:"required,gt=0"`
	OutOrderSn string `json:"out_order_sn" validate:"required"`
	OutUserSn  string `json:"out_user_sn" validate:"required"`
	Num        int    `json:"num" validate:"required,gte=1"`
	OutMobile  string `json:"out_mobile" validate:"omitempty,len=11"`
	ExpireTime int    `json:"expire_time" validate:"omitempty,gte=1,lte=7200"`
}

func (c *CreateOrderParam) Validate() error {
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
