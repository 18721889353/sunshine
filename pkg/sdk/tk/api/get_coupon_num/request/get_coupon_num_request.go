// Package get_coupon_num_request 获取优惠券数量请求包
//
//nolint:revive // 包名包含下划线是为了保持与API路径的一致性
package get_coupon_num_request

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtranslations "github.com/go-playground/validator/v10/translations/zh"

	getcouponnumresponse "github.com/18721889353/sunshine/pkg/sdk/tk/api/get_coupon_num/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"

	"github.com/18721889353/sunshine/pkg/logger"
)

// GetCouponNumRequest 获取优惠券数量请求结构
type GetCouponNumRequest struct {
	core.BaseTkAPIRequest
	Param *GetCouponNumParam
}

// GetURLPath 获取URL路径
func (c *GetCouponNumRequest) GetURLPath() string {
	return "/Api/Coupon/Coupon/findCouponNum"
}

// New 创建新的获取优惠券数量请求
func New() *GetCouponNumRequest {
	request := &GetCouponNumRequest{
		Param: &GetCouponNumParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkAPIClient)
	return request
}

// Execute 执行获取优惠券数量请求
func (c *GetCouponNumRequest) Execute(accessToken string) (*getcouponnumresponse.GetCouponNumResponse, error) {
	responseJSON, err := c.GetClient().Request(c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &getcouponnumresponse.GetCouponNumResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// ExecuteWithContext 带上下文执行获取优惠券数量请求
func (c *GetCouponNumRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*getcouponnumresponse.GetCouponNumResponse, error) {
	responseJSON, err := c.GetClient().RequestWithContext(ctx, c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &getcouponnumresponse.GetCouponNumResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// GetParamObject 获取参数对象
func (c *GetCouponNumRequest) GetParamObject() interface{} {
	return c.Param
}

// GetParams 获取参数
func (c *GetCouponNumRequest) GetParams() *GetCouponNumParam {
	return c.Param
}

// GetCouponNumParam 获取优惠券数量参数结构
type GetCouponNumParam struct {
	CouponID uint64 `json:"coupon_id" validate:"required,gt=0"`
}

func (c *GetCouponNumRequest) Validate() error {
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
