package config

import (
	"context"
	"reflect"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
	"github.com/18721889353/sunshine/pkg/logger"
)

// reloadJwtConfig JWT 配置热更新回调。
// 当 Nacos 配置中的 jwt 段发生变更时，对已持有的默认 JWT Manager 实例执行 Reload
// （注入的指针永不变，消费方无需重注入），并同步更新 Gin/gRPC 两侧的 ignoreMethods。
// 注意：开关（openJwt）由 reloadAppFlags 控制，此处仅维护配置本身，
// 即使开关关闭也保持配置最新，保证开关打开瞬间配置已是最新值。
func reloadJwtConfig(oldCfg, newCfg *Config) {
	ctx := context.Background()
	// 1. 检查 JWT 配置是否发生变更，未变更则跳过
	if reflect.DeepEqual(oldCfg.Jwt, newCfg.Jwt) {
		return
	}

	// 2. 更新 JWT Manager（同一实例 Reload；尚未创建时按新配置创建）。
	// 本回调先于 Set(newCfg) 执行，必须用 newCfg.Jwt 而非 Get()。
	// 失败时保留旧配置并告警，不中断后续 ignoreMethods 同步
	if err := applyJwtConfig(newCfg.Jwt); err != nil {
		logger.WarnWithCtx(ctx, "[config reload] JWT 配置更新失败，保留旧配置",
			logger.String("error", err.Error()),
		)
	}

	// 3. 合并 HTTP 和 gRPC 的忽略路径列表
	// 注意：分配新 slice 避免直接 append 到原配置 slice 上引发 data race
	allIgnoreMethods := make([]string, 0, len(newCfg.Jwt.IgnoreMethods.HTTP)+len(newCfg.Jwt.IgnoreMethods.Grpc))
	allIgnoreMethods = append(allIgnoreMethods, newCfg.Jwt.IgnoreMethods.HTTP...)
	allIgnoreMethods = append(allIgnoreMethods, newCfg.Jwt.IgnoreMethods.Grpc...)
	// 4. 同步更新 Gin 中间件与 gRPC 拦截器的忽略列表（补齐原仅 Gin 侧生效的缺口）
	middleware.SetJwtIgnoreMethods(allIgnoreMethods)
	interceptor.SetJwtIgnoreMethods(allIgnoreMethods)

	logger.InfoWithCtx(ctx, "[config reload] JWT 配置已更新",
		logger.String("signingMethod", newCfg.Jwt.SigningMethod),
		logger.Int("expire", newCfg.Jwt.Expire),
		logger.String("issuer", newCfg.Jwt.Issuer),
		logger.Int("ignoreMethodsCount", len(allIgnoreMethods)),
	)
}
