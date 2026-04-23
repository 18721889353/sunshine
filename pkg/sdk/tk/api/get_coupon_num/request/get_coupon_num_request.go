package get_coupon_num_request

import (
	"context"
	"encoding/json"
	"fmt"

	get_coupon_num_response "github.com/18721889353/sunshine/pkg/sdk/tk/api/get_coupon_num/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtranslations "github.com/go-playground/validator/v10/translations/zh"
)

type GetCouponNumRequest struct {
	core.BaseTkAPIRequest
	Param *GetCouponNumParam
}

func (c *GetCouponNumRequest) GetURLPath() string {
	return "/Api/Coupon/Coupon/findCouponNum"
}

func New() *GetCouponNumRequest {
	request := &GetCouponNumRequest{
		Param: &GetCouponNumParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkAPIClient)
	return request

}

func (c *GetCouponNumRequest) Execute(accessToken string) (*get_coupon_num_response.GetCouponNumResponse, error) {
	responseJson, err := c.GetClient().Request(c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &get_coupon_num_response.GetCouponNumResponse{}
	err = json.Unmarshal([]byte(responseJson), response)
	if err != nil {
		return nil, err
	}
	return response, nil

}

func (c *GetCouponNumRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*get_coupon_num_response.GetCouponNumResponse, error) {
	responseJson, err := c.GetClient().RequestWithContext(ctx, c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &get_coupon_num_response.GetCouponNumResponse{}
	err = json.Unmarshal([]byte(responseJson), response)
	if err != nil {
		return nil, err
	}
	return response, nil

}

func (c *GetCouponNumRequest) GetParamObject() interface{} {
	return c.Param
}

func (c *GetCouponNumRequest) GetParams() *GetCouponNumParam {
	return c.Param
}

type GetCouponNumParam struct {
	CouponId uint64 `json:"coupon_id" validate:"required,gt=0"`
}

func (c *GetCouponNumRequest) Validate() error {
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
