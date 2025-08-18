package get_coupon_info_response

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

type GetCouponInfoResponse struct {
	core.BaseTkApiResponse
	Data *GetCouponInfoData `json:"data"`
}

type UseInfo struct {
	Name string `json:"name"`
}
type ExchangeNotice struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Sort    int    `json:"sort"`
}
type WriteoffNotice struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Sort    int    `json:"sort"`
}
type UseRule struct {
	Name    string `json:"name"`
	Support int    `json:"support"`
}
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
