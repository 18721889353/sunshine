// Package create_order_request 创建订单请求包
//
//nolint:revive // 包名包含下划线是为了保持与API路径的一致性
package create_order_request

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtranslations "github.com/go-playground/validator/v10/translations/zh"

	createorderresponse "github.com/18721889353/sunshine/pkg/sdk/tk/api/create_order/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"

	"github.com/18721889353/sunshine/pkg/logger"
)

// CreateOrderRequest 创建订单请求结构
type CreateOrderRequest struct {
	core.BaseTkAPIRequest
	Param *CreateOrderParam
}

// GetURLPath 获取URL路径
func (c *CreateOrderRequest) GetURLPath() string {
	return "/Api/Order/Order/create"
}

// New 创建新的创建订单请求
func New() *CreateOrderRequest {
	request := &CreateOrderRequest{
		Param: &CreateOrderParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkAPIClient)
	return request
}

// ExecuteWithContext 带上下文执行创建订单请求
func (c *CreateOrderRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*createorderresponse.CreateOrderResponse, error) {
	if err := c.Param.Validate(); err != nil {
		return nil, err
	}
	responseJSON, err := c.GetClient().RequestWithContext(ctx, c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &createorderresponse.CreateOrderResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// GetParamObject 获取参数对象
func (c *CreateOrderRequest) GetParamObject() interface{} {
	return c.Param
}

// GetParams 获取参数
func (c *CreateOrderRequest) GetParams() *CreateOrderParam {
	return c.Param
}

// CreateOrderParam 创建订单参数结构
type CreateOrderParam struct {
	CouponID   int    `json:"coupon_id,omitempty" validate:"required,gt=0"`
	OutOrderSn string `json:"out_order_sn" validate:"required"`
	OutUserSn  string `json:"out_user_sn" validate:"required"`
	Num        int    `json:"num" validate:"required,gte=1"`
	OutMobile  string `json:"out_mobile" validate:"omitempty,len=11"`
	ExpireTime int    `json:"expire_time" validate:"omitempty,gte=1,lte=7200"`
}

// Validate 验证参数
func (c *CreateOrderParam) Validate() error {
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
