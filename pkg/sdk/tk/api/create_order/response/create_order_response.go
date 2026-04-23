package create_order_response

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

type CreateOrderResponse struct {
	core.BaseTkAPIResponse
	Data *CreateOrderData `json:"data"`
}

type CreateOrderData struct {
	OrderSn          string `json:"order_sn"`
	OutOrderSn       string `json:"out_order_sn"`
	OutUserSn        string `json:"out_user_sn"`
	TotalSettlePrice string `json:"total_settle_price"`
}
