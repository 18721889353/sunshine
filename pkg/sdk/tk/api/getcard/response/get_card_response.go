// Package getcardresponse 提供获取卡片API的响应类型
package getcardresponse

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

// GetCardResponse 获取卡片响应
type GetCardResponse struct {
	core.BaseTkAPIResponse
	Data []GetCardData `json:"data"`
}

// GetCardData 获取卡片数据
type GetCardData struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	Pass           string `json:"pass"`
	Crc            string `json:"crc"`
	URL            string `json:"url"`
	URLPass        string `json:"url_pass"`
	BatchNum       string `json:"batch_num"`
	ValidTime      string `json:"valid_time"`
	Type           string `json:"type"`
	H5URL          string `json:"h5_url"`
	CouponMemberID string `json:"coupon_member_id"`
}
