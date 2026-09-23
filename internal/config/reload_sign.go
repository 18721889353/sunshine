package config

import (
	"context"
	"reflect"
	"time"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/logger"
)

// reloadSignConfig Sign 配置热更新回调。
// 当 Nacos 配置中的 sign 段发生变更时，更新全局签名配置，无需重启服务。
// 注意：仅当 OpenSign=true 时生效，否则跳过更新。
func reloadSignConfig(oldCfg, newCfg *Config) {
	ctx := context.Background()
	// 1. 检查签名配置是否发生变更，未变更则跳过
	if reflect.DeepEqual(oldCfg.Sign, newCfg.Sign) {
		return
	}
	// 2. 功能开关未开启时跳过更新
	if !newCfg.App.OpenSign {
		return
	}

	// 3. 合并 HTTP 和 gRPC 的忽略路径列表
	// 注意：分配新 slice 避免直接 append 到原配置 slice 上引发 data race
	allIgnoreUrls := make([]string, 0, len(newCfg.Sign.IgnoreUrls.HTTP)+len(newCfg.Sign.IgnoreUrls.Grpc))
	allIgnoreUrls = append(allIgnoreUrls, newCfg.Sign.IgnoreUrls.HTTP...)
	allIgnoreUrls = append(allIgnoreUrls, newCfg.Sign.IgnoreUrls.Grpc...)
	middleware.SetSignConfig(
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
