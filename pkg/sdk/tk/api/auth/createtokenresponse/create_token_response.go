// Package createtokenresponse 提供创建令牌API的响应类型
package createtokenresponse

import "github.com/18721889353/sunshine/pkg/sdk/tk/core"

// CreateTokenResponse 创建令牌响应
type CreateTokenResponse struct {
	core.BaseTkAPIResponse
	Data CreateTokenData `json:"data"`
}

// CreateTokenData 创建令牌数据
type CreateTokenData struct {
	Token string `json:"token"`
}
