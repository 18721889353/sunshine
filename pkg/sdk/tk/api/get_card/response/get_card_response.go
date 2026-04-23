package get_card_response

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

type GetCardResponse struct {
	core.BaseTkAPIResponse
	Data []GetCardData `json:"data"`
}

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
