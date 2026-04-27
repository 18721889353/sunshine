// Package place_order_request 下单请求包
//
//nolint:revive // 包名包含下划线是为了保持与API路径的一致性
package place_order_request

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtranslations "github.com/go-playground/validator/v10/translations/zh"

	placeorderresponse "github.com/18721889353/sunshine/pkg/sdk/tk/api/place_order/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"

	"github.com/18721889353/sunshine/pkg/logger"
)

// PlaceOrderRequest 下单请求结构
type PlaceOrderRequest struct {
	core.BaseTkAPIRequest
	Param *PlaceOrderParam
}

// GetURLPath 获取URL路径
func (c *PlaceOrderRequest) GetURLPath() string {
	return "/Api/Order/Order/placeOrder"
}

// New 创建新的下单请求
func New() *PlaceOrderRequest {
	request := &PlaceOrderRequest{
		Param: &PlaceOrderParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkAPIClient)
	return request
}

// Execute 执行下单请求
func (c *PlaceOrderRequest) Execute(accessToken string) (*placeorderresponse.PlaceOrderResponse, error) {
	if err := c.Param.Validate(); err != nil {
		return nil, err
	}
	responseJSON, err := c.GetClient().Request(c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &placeorderresponse.PlaceOrderResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// ExecuteWithContext 带上下文执行下单请求
func (c *PlaceOrderRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*placeorderresponse.PlaceOrderResponse, error) {
	if err := c.Param.Validate(); err != nil {
		return nil, err
	}
	responseJSON, err := c.GetClient().RequestWithContext(ctx, c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &placeorderresponse.PlaceOrderResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// GetParamObject 获取参数对象
func (c *PlaceOrderRequest) GetParamObject() interface{} {
	return c.Param
}

// GetParams 获取参数
func (c *PlaceOrderRequest) GetParams() *PlaceOrderParam {
	return c.Param
}

// PlaceOrderParam 下单参数结构
type PlaceOrderParam struct {
	CouponID   int    `json:"coupon_id,omitempty" validate:"required,gt=0"`
	OutOrderSn string `json:"out_order_sn" validate:"required"`
	OutUserSn  string `json:"out_user_sn" validate:"required"`
	Num        int    `json:"num" validate:"required,gte=1"`
	OutMobile  string `json:"out_mobile" validate:"omitempty,len=11"`
	ExpireTime int    `json:"expire_time" validate:"omitempty,gte=1,lte=7200"`
	NotifyURL  string `json:"notify_url" validate:"omitempty,url"`
}

// Validate 验证参数
func (c *PlaceOrderParam) Validate() error {
	zhCh := zh.New()
	validate := validator.New()
	uni := ut.New(zhCh)
	trans, _ := uni.GetTranslator("zh")
	//验证器注册翻译器
	if err := zhtranslations.RegisterDefaultTranslations(validate, trans); err != nil {
		logger.WarnWithCtx(context.Background(), "注册默认翻译失败", logger.Err(err))
	}

	err := validate.Struct(c)
	if err != nil {
		validationErrs, ok := err.(validator.ValidationErrors)
		if !ok {
			return fmt.Errorf("验证错误: %v", err)
		}
		var slice []string
		for _, e := range validationErrs {
			slice = append(slice, e.Translate(trans))
		}
		return fmt.Errorf("%v", slice)
	}
	return nil
}
