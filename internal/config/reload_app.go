package config

import (
	"context"
	"reflect"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/gin/middleware/metrics"
	"github.com/18721889353/sunshine/pkg/grpc/interceptor"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/prof"
)

// reloadAppFlags App 开关字段热更新回调。
// 当 Nacos 配置中的 app 开关字段变更时，同步更新到运行时开关，无需重启服务。
// 涉及的开关：EnableLimit、EnableCircuitBreaker、OpenJwt、OpenSign、EnableToken、
// PprofIPWhiteList、EnableMetrics、EnableHTTPProfile、OpenXSS。
// 注意：中间件/拦截器在启动时已常驻装配，开关通过全局原子变量运行时切换；
// 具体哪个开关改了什么值由统一审计日志（reload_diff）输出，此处只报动作结果。
func reloadAppFlags(oldCfg, newCfg *Config) {
	ctx := context.Background()
	changed := false

	// 1. 逐个检查开关字段是否发生变更，变更则同步到 Gin 中间件与 gRPC 拦截器的运行时开关
	if oldCfg.App.EnableLimit != newCfg.App.EnableLimit {
		middleware.SetSentinelLimitEnabled(newCfg.App.EnableLimit)
		interceptor.SetSentinelLimitEnabled(newCfg.App.EnableLimit)
		changed = true
	}
	if oldCfg.App.EnableCircuitBreaker != newCfg.App.EnableCircuitBreaker {
		middleware.SetSentinelBreakerEnabled(newCfg.App.EnableCircuitBreaker)
		interceptor.SetSentinelBreakerEnabled(newCfg.App.EnableCircuitBreaker)
		changed = true
	}
	if oldCfg.App.OpenJwt != newCfg.App.OpenJwt {
		middleware.SetJwtAuthEnabled(newCfg.App.OpenJwt)
		interceptor.SetJwtEnabled(newCfg.App.OpenJwt)
		changed = true
	}
	if oldCfg.App.OpenSign != newCfg.App.OpenSign {
		middleware.SetSignEnabled(newCfg.App.OpenSign)
		interceptor.SetSignEnabled(newCfg.App.OpenSign)
		changed = true
	}
	if oldCfg.Grpc.EnableToken != newCfg.Grpc.EnableToken {
		interceptor.SetTokenEnabled(newCfg.Grpc.EnableToken)
		changed = true
	}
	if !reflect.DeepEqual(oldCfg.App.PprofIPWhiteList, newCfg.App.PprofIPWhiteList) {
		// 仅生产环境启用了 pprof 鉴权（与启动注册逻辑一致），
		// 热替换鉴权中间件后，新白名单对下次请求即生效
		if newCfg.App.Env == "prod" {
			prof.SetPprofAuth(PprofIPWhitelist(newCfg.App.PprofIPWhiteList))
		}
		changed = true
	}
	// 2. enableMetrics 变更时同步更新 Prometheus 指标采集开关
	if oldCfg.App.EnableMetrics != newCfg.App.EnableMetrics {
		metrics.SetMetricsEnabled(newCfg.App.EnableMetrics)
		changed = true
	}
	// 3. enableHTTPProfile 变更时同步更新 pprof 开关
	if oldCfg.App.EnableHTTPProfile != newCfg.App.EnableHTTPProfile {
		prof.SetPprofEnabled(newCfg.App.EnableHTTPProfile)
		changed = true
	}
	// 4. openXSS 变更时同步更新 XSS 防护中间件开关
	if oldCfg.App.OpenXSS != newCfg.App.OpenXSS {
		middleware.SetXSSEnabled(newCfg.App.OpenXSS)
		changed = true
	}
	// 5. 无变更时直接返回，不输出日志
	if !changed {
		return
	}

	logger.InfoWithCtx(ctx, "[config reload] 应用开关配置已更新")
}
