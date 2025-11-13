package create_token_response

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

type CreateTokenResponse struct {
	core.BaseTkApiResponse
	Data CreateTokenData `json:"data"`
}

type CreateTokenData struct {
	Token string `json:"token"`
}
