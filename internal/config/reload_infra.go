package config

import (
	"context"
	"net/http"
	"time"

	"github.com/18721889353/sunshine/pkg/gocron"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/tracer"
)

// httpServerGetter 用于获取底层 *http.Server 实例的函数，供热更新超时参数使用。
// 必须通过 SetHTTPServerGetter 注册后才可使用，未注册时热更新会跳过并输出警告日志。
var httpServerGetter func() *http.Server

// SetHTTPServerGetter 设置获取 *http.Server 实例的函数，用于 HTTP 超时参数热更新。
// 必须在 NewHTTPServer() 之后调用。
func SetHTTPServerGetter(getter func() *http.Server) {
	httpServerGetter = getter
}

// reloadTracingConfig 链路追踪配置热更新回调。
// 当 Nacos 配置中的 app.enableTrace 或 app.tracingSamplingRate 发生变更时，
// 动态调整采样率，无需重启服务。
// 注意：enableTrace=false 等效于 tracingSamplingRate=0（停止采集），两者取交集。
func reloadTracingConfig(oldCfg, newCfg *Config) {
	ctx := context.Background()

	// 1. 检查链路追踪配置是否发生变更，未变更则跳过
	if oldCfg.App.EnableTrace == newCfg.App.EnableTrace &&
		oldCfg.App.TracingSamplingRate == newCfg.App.TracingSamplingRate {
		return
	}

	// 计算目标采样率：enableTrace=false 时强制为 0
	var targetRate float64
	if !newCfg.App.EnableTrace {
		targetRate = 0
	} else {
		targetRate = newCfg.App.TracingSamplingRate
	}

	tracer.SetSamplingRate(targetRate)

	logger.InfoWithCtx(ctx, "[config reload] 链路追踪配置已更新",
		logger.Bool("enableTrace", newCfg.App.EnableTrace),
		logger.Float64("tracingSamplingRate", newCfg.App.TracingSamplingRate),
		logger.Float64("effectiveRate", targetRate),
	)
}

// reloadOpenCron 定时任务开关热更新回调。
// 当 Nacos 配置中的 app.openCron 发生变更时，暂停或恢复定时任务调度器。
// 注意：仅在启动时 openCron=true 的情况下生效，
// 启动时 openCron=false 则 cronServer 不存在，无法热开启。
func reloadOpenCron(oldCfg, newCfg *Config) {
	ctx := context.Background()
	// 1. 检查定时任务开关是否发生变更，未变更则跳过
	if oldCfg.App.OpenCron == newCfg.App.OpenCron {
		return
	}

	// 2. 根据新配置值暂停或恢复调度器
	if newCfg.App.OpenCron {
		gocron.Resume()
		logger.InfoWithCtx(ctx, "[config reload] 定时任务调度器已恢复",
			logger.Bool("openCron", true),
		)
	} else {
		gocron.Pause()
		logger.InfoWithCtx(ctx, "[config reload] 定时任务调度器已暂停",
			logger.Bool("openCron", false),
		)
	}
}

// reloadHTTPTimeout HTTP 超时参数热更新回调。
// 当 Nacos 配置中的 http.timeout/readTimeout/writeTimeout/readHeaderTimeout/idleTimeout 发生变更时，
// 动态更新 http.Server 的超时字段，无需重启服务。
// 注意：readTimeout 和 writeTimeout 优先使用独立配置，fallback 到 timeout 通用值。
func reloadHTTPTimeout(oldCfg, newCfg *Config) {
	ctx := context.Background()

	oldHTTP := oldCfg.HTTP
	newHTTP := newCfg.HTTP

	// 1. 检查所有超时参数是否发生变更，未变更则跳过
	if oldHTTP.Timeout == newHTTP.Timeout &&
		oldHTTP.ReadTimeout == newHTTP.ReadTimeout &&
		oldHTTP.WriteTimeout == newHTTP.WriteTimeout &&
		oldHTTP.ReadHeaderTimeout == newHTTP.ReadHeaderTimeout &&
		oldHTTP.IdleTimeout == newHTTP.IdleTimeout {
		return
	}

	// 2. httpServerGetter 未注册时跳过更新
	if httpServerGetter == nil {
		logger.WarnWithCtx(ctx, "[config reload] httpServerGetter 未注册，跳过 HTTP 超时配置更新")
		return
	}

	// 3. 获取 http.Server 实例，失败时跳过更新
	srv := httpServerGetter()
	if srv == nil {
		logger.WarnWithCtx(ctx, "[config reload] http.Server 实例为 nil，跳过 HTTP 超时配置更新")
		return
	}

	// 4. 计算 readTimeout 和 writeTimeout（与 createService 逻辑一致：优先用 readTimeout/writeTimeout，fallback 到 timeout）
	newReadTimeout := newHTTP.Timeout
	if newHTTP.ReadTimeout > 0 {
		newReadTimeout = newHTTP.ReadTimeout
	}
	newWriteTimeout := newHTTP.Timeout
	if newHTTP.WriteTimeout > 0 {
		newWriteTimeout = newHTTP.WriteTimeout
	}

	// 5. 逐个参数检查并动态更新 http.Server 超时字段
	if oldHTTP.Timeout != newHTTP.Timeout || oldHTTP.ReadTimeout != newHTTP.ReadTimeout {
		srv.ReadTimeout = time.Duration(newReadTimeout) * time.Second
	}
	if oldHTTP.Timeout != newHTTP.Timeout || oldHTTP.WriteTimeout != newHTTP.WriteTimeout {
		srv.WriteTimeout = time.Duration(newWriteTimeout) * time.Second
	}
	if oldHTTP.ReadHeaderTimeout != newHTTP.ReadHeaderTimeout {
		srv.ReadHeaderTimeout = time.Duration(newHTTP.ReadHeaderTimeout) * time.Second
	}
	if oldHTTP.IdleTimeout != newHTTP.IdleTimeout {
		srv.IdleTimeout = time.Duration(newHTTP.IdleTimeout) * time.Second
	}

	logger.InfoWithCtx(ctx, "[config reload] HTTP 超时配置已更新",
		logger.Int("readTimeout", newReadTimeout),
		logger.Int("writeTimeout", newWriteTimeout),
		logger.Int("readHeaderTimeout", newHTTP.ReadHeaderTimeout),
		logger.Int("idleTimeout", newHTTP.IdleTimeout),
	)
}
