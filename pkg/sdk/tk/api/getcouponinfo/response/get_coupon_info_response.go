// Package getcouponinforesponse 提供获取优惠券信息API的响应类型
package getcouponinforesponse

import (
	"encoding/json"

	"github.com/spf13/cast"

	"github.com/18721889353/sunshine/pkg/sdk/tk/core"
)

// GetCouponInfoResponse 获取优惠券信息响应
type GetCouponInfoResponse struct {
	core.BaseTkAPIResponse
	Data *GetCouponInfoData `json:"data"`
}

// UseInfo 使用信息
type UseInfo struct {
	Name string `json:"name"`
}

// ExchangeNotice 兑换须知
type ExchangeNotice struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Sort    int    `json:"sort"`
}

// UnmarshalJSON implements custom unmarshaling for ExchangeNotice to handle sort field
// that can be either a string or a number
func (e *ExchangeNotice) UnmarshalJSON(data []byte) error {
	var tmp struct {
		Name    string      `json:"name"`
		Content string      `json:"content"`
		Sort    interface{} `json:"sort"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	e.Name = tmp.Name
	e.Content = tmp.Content

	// Handle sort field that can be either string or number
	switch v := tmp.Sort.(type) {
	case string:
		e.Sort = cast.ToInt(v)
	case float64:
		e.Sort = cast.ToInt(v)
	case int:
		e.Sort = v
	case nil:
		e.Sort = 0
	default:
		e.Sort = 0
	}

	return nil
}

// WriteoffNotice 核销须知
type WriteoffNotice struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Sort    int    `json:"sort"`
}

// UnmarshalJSON implements custom unmarshaling for WriteoffNotice to handle sort field
// that can be either a string or a number
func (w *WriteoffNotice) UnmarshalJSON(data []byte) error {
	var tmp struct {
		Name    string      `json:"name"`
		Content string      `json:"content"`
		Sort    interface{} `json:"sort"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	w.Name = tmp.Name
	w.Content = tmp.Content

	// Handle sort field that can be either string or number
	switch v := tmp.Sort.(type) {
	case string:
		w.Sort = cast.ToInt(v)
	case float64:
		w.Sort = cast.ToInt(v)
	case int:
		w.Sort = v
	case nil:
		w.Sort = 0
	default:
		w.Sort = 0
	}

	return nil
}

// UseRule 使用规则
type UseRule struct {
	Name    string `json:"name"`
	Support int    `json:"support"`
}

// GetCouponInfoData 获取优惠券信息数据
type GetCouponInfoData struct {
	ID                       int              `json:"id"`
	CouponName               string           `json:"coupon_name"`
	BrandID                  int              `json:"brand_id"`
	Type                     int              `json:"type"`
	Amount                   string           `json:"amount"`
	Price                    string           `json:"price"`
	UseInfo                  []UseInfo        `json:"use_info"`
	ExchangeNotice           []ExchangeNotice `json:"exchange_notice"`
	RuleTitle                string           `json:"rule_title"`
	Banner                   string           `json:"banner"`
	WriteoffNotice           []WriteoffNotice `json:"writeoff_notice"`
	OffBg                    string           `json:"off_bg"`
	OffStyle                 int              `json:"off_style"`
	UseRule                  []UseRule        `json:"use_rule"`
	IsTimes                  int              `json:"is_times"`
	UseCase                  string           `json:"use_case"`
	LongColourIcon           string           `json:"long_colour_icon"`
	LongGreyIcon             string           `json:"long_grey_icon"`
	SquareColourIcon         string           `json:"square_colour_icon"`
	SquareGreyIcon           string           `json:"square_grey_icon"`
	CircularColourIcon       string           `json:"circular_colour_icon"`
	CircularGreyIcon         string           `json:"circular_grey_icon"`
	WebCouponName            string           `json:"web_coupon_name"`
	CustomLongColourIcon     string           `json:"custom_long_colour_icon"`
	CustomLongGreyIcon       string           `json:"custom_long_grey_icon"`
	CustomSquareColourIcon   string           `json:"custom_square_colour_icon"`
	CustomSquareGreyIcon     string           `json:"custom_square_grey_icon"`
	CustomCircularColourIcon string           `json:"custom_circular_colour_icon"`
	CustomCircularGreyIcon   string           `json:"custom_circular_grey_icon"`
	BrandName                string           `json:"brand_name"`
	TotalSales               int              `json:"total_sales"`
	ExchangeProcess          string           `json:"exchange_process"`
	ExchangeNoticeOptions    int              `json:"exchange_notice_options"`
	ExchangeNoticeURL        string           `json:"exchange_notice_url"`
	FreeNum                  int              `json:"free_num"`
	WriteoffNoticeOptions    int              `json:"writeoff_notice_options"`
	WriteoffNoticeURL        string           `json:"writeoff_notice_url"`
	SettlePrice              string           `json:"settle_price"`
}
