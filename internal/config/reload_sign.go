package config

import (
	"context"
	"reflect"
	"time"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
	"github.com/18721889353/sunshine/pkg/logger"
)

// reloadSignConfig Sign 配置热更新回调。
// 当 Nacos 配置中的 sign 段发生变更时，同步更新 Gin 中间件与 gRPC 拦截器的全局签名配置，无需重启服务。
// 注意：开关（openSign）由 reloadAppFlags 控制，此处仅维护配置本身，
// 即使开关关闭也保持配置最新，保证开关打开瞬间配置已是最新值。
func reloadSignConfig(oldCfg, newCfg *Config) {
	ctx := context.Background()
	// 1. 检查签名配置是否发生变更，未变更则跳过
	if reflect.DeepEqual(oldCfg.Sign, newCfg.Sign) {
		return
	}

	// 2. 合并 HTTP 和 gRPC 的忽略路径列表
	// 注意：分配新 slice 避免直接 append 到原配置 slice 上引发 data race
	allIgnoreUrls := make([]string, 0, len(newCfg.Sign.IgnoreUrls.HTTP)+len(newCfg.Sign.IgnoreUrls.Grpc))
	allIgnoreUrls = append(allIgnoreUrls, newCfg.Sign.IgnoreUrls.HTTP...)
	allIgnoreUrls = append(allIgnoreUrls, newCfg.Sign.IgnoreUrls.Grpc...)
	// 3. 更新 Gin 签名中间件的全局配置（每次请求读取，支持热更新）
	middleware.SetSignConfig(
		allIgnoreUrls,
		false, // ignoreAll 默认关闭
		newCfg.Sign.SignKey,
		time.Duration(newCfg.Sign.SignExpiredTime)*time.Second,
	)
	// 4. 同步更新 gRPC 签名拦截器的全局配置（补齐原仅 Gin 侧生效的缺口）
	interceptor.SetSignConfig(
		allIgnoreUrls,
		false, // ignoreAll 默认关闭
		newCfg.Sign.SignKey,
		time.Duration(newCfg.Sign.SignExpiredTime)*time.Second,
	)

	logger.InfoWithCtx(ctx, "[config reload] 签名配置已更新",
		logger.Int("ignoreUrlsCount", len(allIgnoreUrls)),
		logger.Int("signExpiredTime", newCfg.Sign.SignExpiredTime),
	)
}
