// Package create_order_response 创建订单响应包
//
//nolint:revive // 包名包含下划线是为了保持与API路径的一致性
package create_order_response

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

// CreateOrderResponse 创建订单响应结构
type CreateOrderResponse struct {
	core.BaseTkAPIResponse
	Data *CreateOrderData `json:"data"`
}

// CreateOrderData 创建订单数据结构
type CreateOrderData struct {
	OrderSn          string `json:"order_sn"`
	OutOrderSn       string `json:"out_order_sn"`
	OutUserSn        string `json:"out_user_sn"`
	TotalSettlePrice string `json:"total_settle_price"`
}
