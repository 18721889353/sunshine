package get_coupon_info_request

import (
	"context"
	"encoding/json"
	"fmt"

	get_coupon_info_response "github.com/18721889353/sunshine/pkg/sdk/tk/api/get_coupon_info/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtranslations "github.com/go-playground/validator/v10/translations/zh"
)

type GetCouponInfoRequest struct {
	core.BaseTkAPIRequest
	Param *GetCouponInfoParam
}

func (c *GetCouponInfoRequest) GetURLPath() string {
	return "/Api/Coupon/Coupon/detail"
}

func New() *GetCouponInfoRequest {
	request := &GetCouponInfoRequest{
		Param: &GetCouponInfoParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkAPIClient)
	return request

}

func (c *GetCouponInfoRequest) Execute(accessToken string) (*get_coupon_info_response.GetCouponInfoResponse, error) {
	responseJson, err := c.GetClient().Request(c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &get_coupon_info_response.GetCouponInfoResponse{}
	err = json.Unmarshal([]byte(responseJson), response)
	if err != nil {
		return nil, err
	}
	return response, nil

}

func (c *GetCouponInfoRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*get_coupon_info_response.GetCouponInfoResponse, error) {
	responseJson, err := c.GetClient().RequestWithContext(ctx, c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &get_coupon_info_response.GetCouponInfoResponse{}
	err = json.Unmarshal([]byte(responseJson), response)
	if err != nil {
		return nil, err
	}
	return response, nil

}

func (c *GetCouponInfoRequest) GetParamObject() interface{} {
	return c.Param
}

func (c *GetCouponInfoRequest) GetParams() *GetCouponInfoParam {
	return c.Param
}

type GetCouponInfoParam struct {
	CouponId uint64 `json:"id" validate:"required,gt=0"`
}

func (c *GetCouponInfoRequest) Validate() error {
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
