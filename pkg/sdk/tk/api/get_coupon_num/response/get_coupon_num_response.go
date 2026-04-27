// Package get_coupon_num_response 获取优惠券数量响应包
//
//nolint:revive // 包名包含下划线是为了保持与API路径的一致性
package get_coupon_num_response

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

// GetCouponNumResponse 获取优惠券数量响应结构
type GetCouponNumResponse struct {
	core.BaseTkAPIResponse
	Data *GetCouponNumData `json:"data"`
}

// GetCouponNumData 获取优惠券数量数据结构
type GetCouponNumData struct {
	// 审核结果
	Num uint64 `json:"num"`
}
