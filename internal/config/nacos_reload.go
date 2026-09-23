// Package config 提供配置初始化和全局配置访问。
package config

import (
	"context"
	"database/sql"
	"net/http"
	"reflect"
	"time"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	v5 "github.com/golang-jwt/jwt/v5"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/gin/middleware/metrics"
	"github.com/18721889353/sunshine/pkg/gin/prof"
	"github.com/18721889353/sunshine/pkg/gocron"
	"github.com/18721889353/sunshine/pkg/jwt"
	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sgorm/glog"
	"github.com/18721889353/sunshine/pkg/tracer"
)

// sqlDBGetter 用于获取底层 sql.DB 实例的函数，避免循环导入
var sqlDBGetter func() (*sql.DB, error)

// httpServerGetter 用于获取底层 *http.Server 实例的函数，供热更新超时参数使用
var httpServerGetter func() *http.Server

// RegisterBuiltinReloads 注册框架内置的配置热更新回调。
// 包括：Sentinel 限流/熔断规则、JWT 配置、Sign 配置、App 开关字段、数据库连接池、
// GORM 日志、链路追踪、定时任务、HTTP 超时。
//
// 注意：必须在 database.InitDB 之后调用，否则数据库回调无法正常工作。
func RegisterBuiltinReloads() {
	// 1. Sentinel 限流规则热更新
	RegisterReload(reloadSentinelFlowRules)

	// 2. Sentinel 熔断规则热更新
	RegisterReload(reloadSentinelBreakerRules)

	// 3. JWT 配置热更新（包括 ignoreMethods）
	RegisterReload(reloadJwtConfig)

	// 4. Sign 配置热更新
	RegisterReload(reloadSignConfig)

	// 5. App 开关字段热更新
	RegisterReload(reloadAppFlags)

	// 6. 数据库连接池参数热更新
	RegisterReload(reloadDatabasePool)

	// 7. GORM 日志配置热更新
	RegisterReload(reloadGormLogger)

	// 8. 链路追踪配置热更新
	RegisterReload(reloadTracingConfig)

	// 9. 定时任务开关热更新
	RegisterReload(reloadOpenCron)

	// 10. HTTP 超时参数热更新
	RegisterReload(reloadHTTPTimeout)
}

// reloadSentinelFlowRules Sentinel 限流规则热更新回调。
// 当 Nacos 配置中的 sentinel.limitRules 发生变更时，动态重载限流规则。
func reloadSentinelFlowRules(oldCfg, newCfg *Config) {
	ctx := context.Background()
	if reflect.DeepEqual(oldCfg.Sentinel.LimitRules, newCfg.Sentinel.LimitRules) {
		return
	}
	if !newCfg.App.EnableLimit {
		return
	}

	rules := buildFlowRules(newCfg)
	if err := middleware.ReloadFlowRules(rules); err != nil {
		logger.WarnWithCtx(ctx, "[config reload] sentinel flow rules reload failed",
			logger.Err(err),
		)
		return
	}
	logger.InfoWithCtx(ctx, "[config reload] sentinel flow rules updated",
		logger.Int("ruleCount", len(rules)),
	)
}

// reloadSentinelBreakerRules Sentinel 熔断规则热更新回调。
// 当 Nacos 配置中的 sentinel.breakerRules 发生变更时，动态重载熔断规则。
func reloadSentinelBreakerRules(oldCfg, newCfg *Config) {
	ctx := context.Background()
	if reflect.DeepEqual(oldCfg.Sentinel.BreakerRules, newCfg.Sentinel.BreakerRules) {
		return
	}
	if !newCfg.App.EnableCircuitBreaker {
		return
	}

	rules := buildBreakerRules(newCfg)
	if err := middleware.ReloadBreakerRules(rules); err != nil {
		logger.WarnWithCtx(ctx, "[config reload] sentinel breaker rules reload failed",
			logger.Err(err),
		)
		return
	}
	logger.InfoWithCtx(ctx, "[config reload] sentinel breaker rules updated",
		logger.Int("ruleCount", len(rules)),
	)
}

// buildFlowRules 从配置构建 Sentinel 限流规则列表。
func buildFlowRules(cfg *Config) []*flow.Rule {
	var rules []*flow.Rule
	for _, r := range cfg.Sentinel.LimitRules {
		rules = append(rules, &flow.Rule{
			Resource:               r.Resource,
			TokenCalculateStrategy: middleware.ParseTokenCalculateStrategy(r.TokenCalculateStrategy),
			ControlBehavior:        middleware.ParseControlBehavior(r.ControlBehavior),
			Threshold:              r.Threshold,
			StatIntervalInMs:       uint32(r.StatIntervalInMs),
		})
	}
	return rules
}

// buildBreakerRules 从配置构建 Sentinel 熔断规则列表。
func buildBreakerRules(cfg *Config) []*circuitbreaker.Rule {
	var rules []*circuitbreaker.Rule
	for _, r := range cfg.Sentinel.BreakerRules {
		rules = append(rules, &circuitbreaker.Rule{
			Resource:         r.Resource,
			Strategy:         middleware.ParseBreakerStrategy(r.Strategy),
			RetryTimeoutMs:   uint32(r.RetryTimeoutMs),
			MinRequestAmount: uint64(r.MinRequestAmount),
			StatIntervalMs:   uint32(r.StatIntervalMs),
			Threshold:        r.Threshold,
		})
	}
	return rules
}

// reloadJwtConfig JWT 配置热更新回调。
// 当 Nacos 配置中的 jwt 段发生变更时，重新初始化 JWT 全局配置，并更新 ignoreMethods。
func reloadJwtConfig(oldCfg, newCfg *Config) {
	ctx := context.Background()
	if reflect.DeepEqual(oldCfg.Jwt, newCfg.Jwt) {
		return
	}
	if !newCfg.App.OpenJwt {
		return
	}

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

	// 更新 ignoreMethods（支持热更新）
	allIgnoreMethods := append(newCfg.Jwt.IgnoreMethods.HTTP, newCfg.Jwt.IgnoreMethods.Grpc...)
	middleware.SetJwtIgnoreMethods(allIgnoreMethods)

	logger.InfoWithCtx(ctx, "[config reload] jwt config updated",
		logger.String("signingMethod", newCfg.Jwt.SigningMethod),
		logger.Int("expire", newCfg.Jwt.Expire),
		logger.String("issuer", newCfg.Jwt.Issuer),
		logger.Int("ignoreMethodsCount", len(allIgnoreMethods)),
	)
}

// reloadSignConfig Sign 配置热更新回调。
// 当 Nacos 配置中的 sign 段发生变更时，更新全局签名配置。
func reloadSignConfig(oldCfg, newCfg *Config) {
	ctx := context.Background()
	if reflect.DeepEqual(oldCfg.Sign, newCfg.Sign) {
		return
	}
	if !newCfg.App.OpenSign {
		return
	}

	// 合并 HTTP 和 gRPC 的 ignoreUrls
	allIgnoreUrls := append(newCfg.Sign.IgnoreUrls.HTTP, newCfg.Sign.IgnoreUrls.Grpc...)
	middleware.SetSignConfig(
		allIgnoreUrls,
		false, // ignoreAll 默认关闭
		newCfg.Sign.SignKey,
		time.Duration(newCfg.Sign.SignExpiredTime)*time.Second,
	)

	logger.InfoWithCtx(ctx, "[config reload] sign config updated",
		logger.Int("ignoreUrlsCount", len(allIgnoreUrls)),
		logger.Int("signExpiredTime", newCfg.Sign.SignExpiredTime),
	)
}

// reloadAppFlags App 开关字段热更新回调。
// 当 Nacos 配置中的 app 开关字段变更时，直接更新到全局配置。
func reloadAppFlags(oldCfg, newCfg *Config) {
	ctx := context.Background()
	changed := false

	if oldCfg.App.EnableLimit != newCfg.App.EnableLimit {
		logger.InfoWithCtx(ctx, "[config reload] app.enableLimit changed",
			logger.Bool("old", oldCfg.App.EnableLimit),
			logger.Bool("new", newCfg.App.EnableLimit),
		)
		changed = true
	}
	if oldCfg.App.EnableCircuitBreaker != newCfg.App.EnableCircuitBreaker {
		logger.InfoWithCtx(ctx, "[config reload] app.enableCircuitBreaker changed",
			logger.Bool("old", oldCfg.App.EnableCircuitBreaker),
			logger.Bool("new", newCfg.App.EnableCircuitBreaker),
		)
		changed = true
	}
	if oldCfg.App.OpenJwt != newCfg.App.OpenJwt {
		logger.InfoWithCtx(ctx, "[config reload] app.openJwt changed",
			logger.Bool("old", oldCfg.App.OpenJwt),
			logger.Bool("new", newCfg.App.OpenJwt),
		)
		changed = true
	}
	if oldCfg.App.OpenSign != newCfg.App.OpenSign {
		logger.InfoWithCtx(ctx, "[config reload] app.openSign changed",
			logger.Bool("old", oldCfg.App.OpenSign),
			logger.Bool("new", newCfg.App.OpenSign),
		)
		changed = true
	}
	if !reflect.DeepEqual(oldCfg.App.PprofIPWhiteList, newCfg.App.PprofIPWhiteList) {
		logger.InfoWithCtx(ctx, "[config reload] app.pprofIPWhiteList changed",
			logger.Any("old", oldCfg.App.PprofIPWhiteList),
			logger.Any("new", newCfg.App.PprofIPWhiteList),
		)
		changed = true
	}
	// enableMetrics 热更新
	if oldCfg.App.EnableMetrics != newCfg.App.EnableMetrics {
		metrics.SetMetricsEnabled(newCfg.App.EnableMetrics)
		logger.InfoWithCtx(ctx, "[config reload] app.enableMetrics changed",
			logger.Bool("old", oldCfg.App.EnableMetrics),
			logger.Bool("new", newCfg.App.EnableMetrics),
		)
		changed = true
	}
	// enableHTTPProfile 热更新
	if oldCfg.App.EnableHTTPProfile != newCfg.App.EnableHTTPProfile {
		prof.SetPprofEnabled(newCfg.App.EnableHTTPProfile)
		logger.InfoWithCtx(ctx, "[config reload] app.enableHTTPProfile changed",
			logger.Bool("old", oldCfg.App.EnableHTTPProfile),
			logger.Bool("new", newCfg.App.EnableHTTPProfile),
		)
		changed = true
	}
	// openXSS 热更新
	if oldCfg.App.OpenXSS != newCfg.App.OpenXSS {
		middleware.SetXSSEnabled(newCfg.App.OpenXSS)
		logger.InfoWithCtx(ctx, "[config reload] app.openXSS changed",
			logger.Bool("old", oldCfg.App.OpenXSS),
			logger.Bool("new", newCfg.App.OpenXSS),
		)
		changed = true
	}
	if !changed {
		return
	}

	logger.InfoWithCtx(ctx, "[config reload] app flags updated")
}

// reloadDatabasePool 数据库连接池参数热更新回调。
// 当 Nacos 配置中的 database.mysql 连接池参数发生变更时，动态调整连接池配置。
// 支持的参数：maxIdleConns、maxOpenConns、connMaxLifetime、maxIdleTime。
func reloadDatabasePool(oldCfg, newCfg *Config) {
	ctx := context.Background()
	oldMysql := oldCfg.Database.Mysql
	newMysql := newCfg.Database.Mysql

	if oldMysql.MaxIdleConns == newMysql.MaxIdleConns &&
		oldMysql.MaxOpenConns == newMysql.MaxOpenConns &&
		oldMysql.ConnMaxLifetime == newMysql.ConnMaxLifetime &&
		oldMysql.MaxIdleTime == newMysql.MaxIdleTime {
		return
	}

	if sqlDBGetter == nil {
		logger.WarnWithCtx(ctx, "[config reload] sqlDBGetter not set, skip database pool config update")
		return
	}

	sqlDB, err := sqlDBGetter()
	if err != nil {
		logger.WarnWithCtx(ctx, "[config reload] failed to get sql.DB",
			logger.Err(err),
		)
		return
	}
	if sqlDB == nil {
		logger.WarnWithCtx(ctx, "[config reload] sql.DB is nil, skip database pool config update")
		return
	}

	if oldMysql.MaxIdleConns != newMysql.MaxIdleConns {
		sqlDB.SetMaxIdleConns(newMysql.MaxIdleConns)
	}
	if oldMysql.MaxOpenConns != newMysql.MaxOpenConns {
		sqlDB.SetMaxOpenConns(newMysql.MaxOpenConns)
	}
	if oldMysql.ConnMaxLifetime != newMysql.ConnMaxLifetime {
		sqlDB.SetConnMaxLifetime(time.Duration(newMysql.ConnMaxLifetime) * time.Minute)
	}
	if oldMysql.MaxIdleTime != newMysql.MaxIdleTime {
		sqlDB.SetConnMaxIdleTime(time.Duration(newMysql.MaxIdleTime) * time.Minute)
	}

	logger.InfoWithCtx(ctx, "[config reload] database pool config updated",
		logger.Int("maxIdleConns", newMysql.MaxIdleConns),
		logger.Int("maxOpenConns", newMysql.MaxOpenConns),
		logger.Int("connMaxLifetime", newMysql.ConnMaxLifetime),
		logger.Int("maxIdleTime", newMysql.MaxIdleTime),
	)
}

// reloadGormLogger GORM 日志配置热更新回调。
// 当 Nacos 配置中的 database.mysql.enableLog 或 slowQueryThresholdMs 发生变更时，
// 动态调整 GORM 日志行为，无需重启服务。
func reloadGormLogger(oldCfg, newCfg *Config) {
	ctx := context.Background()
	oldMysql := oldCfg.Database.Mysql
	newMysql := newCfg.Database.Mysql

	if oldMysql.EnableLog == newMysql.EnableLog &&
		oldMysql.SlowQueryThresholdMs == newMysql.SlowQueryThresholdMs {
		return
	}

	// 更新 enableLog
	glog.DynamicGormLogger.SetEnableLog(newMysql.EnableLog)

	// 更新 slowThreshold
	if newMysql.SlowQueryThresholdMs > 0 {
		glog.DynamicGormLogger.SetSlowThreshold(time.Duration(newMysql.SlowQueryThresholdMs) * time.Millisecond)
	} else {
		glog.DynamicGormLogger.SetSlowThreshold(0)
	}

	logger.InfoWithCtx(ctx, "[config reload] gorm logger config updated",
		logger.Bool("enableLog", newMysql.EnableLog),
		logger.Int("slowQueryThresholdMs", newMysql.SlowQueryThresholdMs),
	)
}

// SetSQLDBGetter 设置获取 sql.DB 实例的函数，用于数据库连接池热更新。
// 必须在 database.InitDB() 之后调用。
func SetSQLDBGetter(getter func() (*sql.DB, error)) {
	sqlDBGetter = getter
}

// reloadTracingConfig 链路追踪配置热更新回调。
// 当 Nacos 配置中的 app.enableTrace 或 app.tracingSamplingRate 发生变更时，
// 动态调整采样率，无需重启服务。
// enableTrace=false 等效于 tracingSamplingRate=0（停止采集）。
func reloadTracingConfig(oldCfg, newCfg *Config) {
	ctx := context.Background()

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

	logger.InfoWithCtx(ctx, "[config reload] tracing config updated",
		logger.Bool("enableTrace", newCfg.App.EnableTrace),
		logger.Float64("tracingSamplingRate", newCfg.App.TracingSamplingRate),
		logger.Float64("effectiveRate", targetRate),
	)
}

// SetHTTPServerGetter 设置获取 *http.Server 实例的函数，用于 HTTP 超时参数热更新。
// 必须在 NewHTTPServer() 之后调用。
func SetHTTPServerGetter(getter func() *http.Server) {
	httpServerGetter = getter
}

// reloadHTTPTimeout HTTP 超时参数热更新回调。
// 当 Nacos 配置中的 http.timeout/readTimeout/writeTimeout/readHeaderTimeout/idleTimeout 发生变更时，
// 动态更新 http.Server 的超时字段，无需重启服务。
func reloadHTTPTimeout(oldCfg, newCfg *Config) {
	ctx := context.Background()

	oldHTTP := oldCfg.HTTP
	newHTTP := newCfg.HTTP

	// 调试日志：打印新旧配置值
	logger.InfoWithCtx(ctx, "[config reload] http timeout check",
		logger.Int("old.Timeout", oldHTTP.Timeout),
		logger.Int("new.Timeout", newHTTP.Timeout),
		logger.Int("old.ReadTimeout", oldHTTP.ReadTimeout),
		logger.Int("new.ReadTimeout", newHTTP.ReadTimeout),
		logger.Int("old.WriteTimeout", oldHTTP.WriteTimeout),
		logger.Int("new.WriteTimeout", newHTTP.WriteTimeout),
		logger.Int("old.ReadHeaderTimeout", oldHTTP.ReadHeaderTimeout),
		logger.Int("new.ReadHeaderTimeout", newHTTP.ReadHeaderTimeout),
		logger.Int("old.IdleTimeout", oldHTTP.IdleTimeout),
		logger.Int("new.IdleTimeout", newHTTP.IdleTimeout),
	)

	if oldHTTP.Timeout == newHTTP.Timeout &&
		oldHTTP.ReadTimeout == newHTTP.ReadTimeout &&
		oldHTTP.WriteTimeout == newHTTP.WriteTimeout &&
		oldHTTP.ReadHeaderTimeout == newHTTP.ReadHeaderTimeout &&
		oldHTTP.IdleTimeout == newHTTP.IdleTimeout {
		logger.InfoWithCtx(ctx, "[config reload] http timeout unchanged, skip")
		return
	}

	if httpServerGetter == nil {
		logger.WarnWithCtx(ctx, "[config reload] httpServerGetter not set, skip http timeout config update")
		return
	}

	srv := httpServerGetter()
	if srv == nil {
		logger.WarnWithCtx(ctx, "[config reload] http.Server is nil, skip http timeout config update")
		return
	}

	// 计算 readTimeout 和 writeTimeout（与 createService 逻辑一致：优先用 readTimeout/writeTimeout，fallback 到 timeout）
	newReadTimeout := newHTTP.Timeout
	if newHTTP.ReadTimeout > 0 {
		newReadTimeout = newHTTP.ReadTimeout
	}
	newWriteTimeout := newHTTP.Timeout
	if newHTTP.WriteTimeout > 0 {
		newWriteTimeout = newHTTP.WriteTimeout
	}

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

	logger.InfoWithCtx(ctx, "[config reload] http timeout config updated",
		logger.Int("readTimeout", newReadTimeout),
		logger.Int("writeTimeout", newWriteTimeout),
		logger.Int("readHeaderTimeout", newHTTP.ReadHeaderTimeout),
		logger.Int("idleTimeout", newHTTP.IdleTimeout),
	)
}

// reloadOpenCron 定时任务开关热更新回调。
// 当 Nacos 配置中的 app.openCron 发生变更时，暂停或恢复定时任务调度器。
// 注意：仅在启动时 openCron=true 的情况下生效，
// 启动时 openCron=false 则 cronServer 不存在，无法热开启。
func reloadOpenCron(oldCfg, newCfg *Config) {
	ctx := context.Background()
	if oldCfg.App.OpenCron == newCfg.App.OpenCron {
		return
	}

	if newCfg.App.OpenCron {
		gocron.Resume()
		logger.InfoWithCtx(ctx, "[config reload] cron scheduler resumed",
			logger.Bool("openCron", true),
		)
	} else {
		gocron.Pause()
		logger.InfoWithCtx(ctx, "[config reload] cron scheduler paused",
			logger.Bool("openCron", false),
		)
	}
}
