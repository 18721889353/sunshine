package main

import (
	"context"
	"encoding/json"
	"fmt"

	get_coupon_num_request "github.com/18721889353/sunshine/pkg/sdk/tk/api/get_coupon_num/request"

	"github.com/18721889353/sunshine/pkg/sdk/tk/core"
)

func main() {
	tkConfig := core.NewTkConfig(
		core.WithAppId(""),
		core.WithAppSecret(""),
		//core.WithOpenRequestUrl("https://new-test.tongkask.com"),
		//core.WithHttpReadTimeout(10000),
	)

	accessToken, err := core.GetAccessToken(&core.GetAccessTokenParam{
		AppId:     tkConfig.AppId,
		AppSecret: tkConfig.AppSecret,
	})
	fmt.Println(accessToken, err)
	if err != nil {
		panic(err)
	}

	request := get_coupon_num_request.New()
	param := request.GetParams()
	param.CouponId = 1064
	res, err := request.ExecuteWithContext(context.Background(), accessToken)
	//res, err := request.Execute(accessToken)
	if err != nil {
		panic(err)
	}

	//request := get_coupon_info_request.New()
	//param1 := request.GetParams()
	//param1.CouponId = 1064
	//res, err := request.ExecuteWithContext(context.Background(), accessToken)
	//res, err := request.Execute(context.Background(), accessToken)
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Printf("Response: %+v\n", res)
	//marshal, err := json.Marshal(res)
	//fmt.Println(err, string(marshal))

	//request := create_order_request.New()
	//param := request.GetParams()
	//param.CouponID = 1064
	//param.Num = 1
	//param.OutOrderSn = "20230401"
	//param.OutMobile = "18888888888"
	//param.ExpireTime = 1
	//param.OutUserSn = "20230401"
	//res, err := request.Execute(accessToken)
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Printf("Response: %+v\n", res)
	//marshal, err := json.Marshal(res)
	//fmt.Println(err, string(marshal))

	//request := get_card_request.New()
	//param := request.GetParams()
	//param.OrderSn = "2507291705389285325512"
	//res, err := request.Execute(accessToken)
	//if err != nil {
	//	panic(err)
	//}
	fmt.Printf("Response: %+v\n", res)
	marshal, err := json.Marshal(res)
	fmt.Println(err, string(marshal))

}
