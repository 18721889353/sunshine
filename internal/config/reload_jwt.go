package config

import (
	"context"
	"reflect"
	"time"

	v5 "github.com/golang-jwt/jwt/v5"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/jwt"
	"github.com/18721889353/sunshine/pkg/logger"
)

// reloadJwtConfig JWT 配置热更新回调。
// 当 Nacos 配置中的 jwt 段发生变更时，重新初始化 JWT 全局配置，并更新 ignoreMethods。
// 注意：仅当 OpenJwt=true 时生效，否则跳过更新。
func reloadJwtConfig(oldCfg, newCfg *Config) {
	ctx := context.Background()
	// 1. 检查 JWT 配置是否发生变更，未变更则跳过
	if reflect.DeepEqual(oldCfg.Jwt, newCfg.Jwt) {
		return
	}
	// 2. 功能开关未开启时跳过更新
	if !newCfg.App.OpenJwt {
		return
	}

	// 3. 根据配置选择签名算法，支持 HS256/HS384/HS512
	var sm *v5.SigningMethodHMAC
	switch newCfg.Jwt.SigningMethod {
	case "HS256":
		sm = jwt.HS256
	case "HS384":
		sm = jwt.HS384
	default:
		sm = jwt.HS512
	}
	jwt.Init(
		jwt.WithExpire(time.Minute*time.Duration(newCfg.Jwt.Expire)),
		jwt.WithSigningKey(newCfg.Jwt.SigningKey),
		jwt.WithSigningMethod(sm),
		jwt.WithIssuer(newCfg.Jwt.Issuer),
	)

	// 4. 合并 HTTP 和 gRPC 的忽略路径列表
	// 注意：分配新 slice 避免直接 append 到原配置 slice 上引发 data race
	allIgnoreMethods := make([]string, 0, len(newCfg.Jwt.IgnoreMethods.HTTP)+len(newCfg.Jwt.IgnoreMethods.Grpc))
	allIgnoreMethods = append(allIgnoreMethods, newCfg.Jwt.IgnoreMethods.HTTP...)
	allIgnoreMethods = append(allIgnoreMethods, newCfg.Jwt.IgnoreMethods.Grpc...)
	middleware.SetJwtIgnoreMethods(allIgnoreMethods)

	logger.InfoWithCtx(ctx, "[config reload] JWT 配置已更新",
		logger.String("signingMethod", newCfg.Jwt.SigningMethod),
		logger.Int("expire", newCfg.Jwt.Expire),
		logger.String("issuer", newCfg.Jwt.Issuer),
		logger.Int("ignoreMethodsCount", len(allIgnoreMethods)),
	)
}
