package place_order_response

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

type PlaceOrderResponse struct {
	core.BaseTkApiResponse
	Data *PlaceOrderData `json:"data"`
}

type PlaceOrderData struct {
	OrderSn          string `json:"order_sn"`
	OutOrderSn       string `json:"out_order_sn"`
	OutUserSn        string `json:"out_user_sn"`
	TotalSettlePrice string `json:"total_settle_price"`
}
