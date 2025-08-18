package get_coupon_num_response

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

type GetCouponNumResponse struct {
	core.BaseTkApiResponse
	Data *GetCouponNumData `json:"data"`
}

type GetCouponNumData struct {
	// 审核结果
	Num uint64 `json:"num"`
}
