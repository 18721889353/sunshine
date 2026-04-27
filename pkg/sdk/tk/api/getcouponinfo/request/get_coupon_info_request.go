// Package getcouponinforequest 获取优惠券信息请求包
package getcouponinforequest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtranslations "github.com/go-playground/validator/v10/translations/zh"

	getcouponinforesponse "github.com/18721889353/sunshine/pkg/sdk/tk/api/getcouponinfo/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"

	"github.com/18721889353/sunshine/pkg/logger"
)

// GetCouponInfoRequest 获取优惠券信息请求结构
type GetCouponInfoRequest struct {
	core.BaseTkAPIRequest
	Param *GetCouponInfoParam
}

// GetURLPath 获取URL路径
func (c *GetCouponInfoRequest) GetURLPath() string {
	return "/Api/Coupon/Coupon/detail"
}

// New 创建新的获取优惠券信息请求
func New() *GetCouponInfoRequest {
	request := &GetCouponInfoRequest{
		Param: &GetCouponInfoParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkAPIClient)
	return request
}

// Execute 执行获取优惠券信息请求
func (c *GetCouponInfoRequest) Execute(accessToken string) (*getcouponinforesponse.GetCouponInfoResponse, error) {
	responseJSON, err := c.GetClient().Request(c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &getcouponinforesponse.GetCouponInfoResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// ExecuteWithContext 带上下文执行获取优惠券信息请求
func (c *GetCouponInfoRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*getcouponinforesponse.GetCouponInfoResponse, error) {
	responseJSON, err := c.GetClient().RequestWithContext(ctx, c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &getcouponinforesponse.GetCouponInfoResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// GetParamObject 获取参数对象
func (c *GetCouponInfoRequest) GetParamObject() interface{} {
	return c.Param
}

// GetParams 获取参数
func (c *GetCouponInfoRequest) GetParams() *GetCouponInfoParam {
	return c.Param
}

// GetCouponInfoParam 获取优惠券信息参数结构
type GetCouponInfoParam struct {
	CouponID uint64 `json:"id" validate:"required,gt=0"`
}

// Validate 验证参数
func (c *GetCouponInfoRequest) Validate() error {
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
