// Package getcardrequest provides request types for get card API
package getcardrequest

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtranslations "github.com/go-playground/validator/v10/translations/zh"

	getcardresponse "github.com/18721889353/sunshine/pkg/sdk/tk/api/getcard/response"
	"github.com/18721889353/sunshine/pkg/sdk/tk/core"
)

// GetCardRequest 获取卡片请求
type GetCardRequest struct {
	core.BaseTkAPIRequest
	Param *GetCardParam
}

// GetURLPath 获取URL路径
func (c *GetCardRequest) GetURLPath() string {
	return "/Api/Order/Order/getCard"
}

// New 创建新的获取卡片请求实例
func New() *GetCardRequest {
	request := &GetCardRequest{
		Param: &GetCardParam{},
	}
	request.SetConfig(core.GetTkConfig())
	request.SetClient(core.DefaultTkAPIClient)
	return request
}

// Execute 执行获取卡片请求
func (c *GetCardRequest) Execute(accessToken string) (*getcardresponse.GetCardResponse, error) {
	if err := c.Param.Validate(); err != nil {
		return nil, err
	}
	responseJSON, err := c.GetClient().Request(c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &getcardresponse.GetCardResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// ExecuteWithContext 带上下文执行获取卡片请求
func (c *GetCardRequest) ExecuteWithContext(ctx context.Context, accessToken string) (*getcardresponse.GetCardResponse, error) {
	if err := c.Param.Validate(); err != nil {
		return nil, err
	}
	responseJSON, err := c.GetClient().RequestWithContext(ctx, c, accessToken)
	if err != nil {
		return nil, err
	}
	response := &getcardresponse.GetCardResponse{}
	err = json.Unmarshal([]byte(responseJSON), response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// GetParamObject 获取参数对象
func (c *GetCardRequest) GetParamObject() interface{} {
	return c.Param
}

// GetParams 获取参数
func (c *GetCardRequest) GetParams() *GetCardParam {
	return c.Param
}

// GetCardParam 获取卡片参数
type GetCardParam struct {
	OrderSn string `json:"order_sn" validate:"required"`
}

// Validate 验证参数
func (c *GetCardParam) Validate() error {
	zhCh := zh.New()
	validate := validator.New()
	uni := ut.New(zhCh)
	trans, _ := uni.GetTranslator("zh")
	//验证器注册翻译器
	if err := zhtranslations.RegisterDefaultTranslations(validate, trans); err != nil {
		return fmt.Errorf("register translations error: %w", err)
	}

	err := validate.Struct(c)
	if err != nil {
		validationErrs, ok := err.(validator.ValidationErrors)
		if !ok {
			return fmt.Errorf("validation error: %w", err)
		}
		var slice []string
		for _, e := range validationErrs {
			slice = append(slice, e.Translate(trans))
		}
		return fmt.Errorf("%v", slice)
	}
	return nil
}
