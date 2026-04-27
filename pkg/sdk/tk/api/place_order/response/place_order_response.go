// Package place_order_response 下单响应包
//
//nolint:revive // 包名包含下划线是为了保持与API路径的一致性
package place_order_response

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

// PlaceOrderResponse 下单响应结构
type PlaceOrderResponse struct {
	core.BaseTkAPIResponse
	Data *PlaceOrderData `json:"data"`
}

// PlaceOrderData 下单数据结构
type PlaceOrderData struct {
	OrderSn          string `json:"order_sn"`
	OutOrderSn       string `json:"out_order_sn"`
	OutUserSn        string `json:"out_user_sn"`
	TotalSettlePrice string `json:"total_settle_price"`
}
