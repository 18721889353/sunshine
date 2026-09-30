package config

import (
	"context"
	"reflect"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"

	"github.com/18721889353/sunshine/pkg/gin/middleware"
	"github.com/18721889353/sunshine/pkg/logger"
)

// reloadSentinelFlowRules Sentinel 限流规则热更新回调。
// 当 Nacos 配置中的 sentinel.limitRules 或 app.enableLimit 发生变更时，动态重载限流规则，无需重启服务。
// 规则按开关构建：enableLimit=false 时清空规则，彻底关闭限流；开关本身由 reloadAppFlags 同步切换。
func reloadSentinelFlowRules(oldCfg, newCfg *Config) {
	ctx := context.Background()
	// 1. 检查限流规则或开关是否发生变更，均未变更则跳过
	if reflect.DeepEqual(oldCfg.Sentinel.LimitRules, newCfg.Sentinel.LimitRules) &&
		oldCfg.App.EnableLimit == newCfg.App.EnableLimit {
		return
	}

	// 2. 按开关构建规则，开关关闭时用空规则清空全局规则
	var rules []*flow.Rule
	if newCfg.App.EnableLimit {
		rules = buildFlowRules(newCfg)
	}

	// 3. flow.LoadRules 支持空规则清空（middleware.ReloadFlowRules 对空规则短路跳过，故直接调用）
	if _, err := flow.LoadRules(rules); err != nil {
		logger.WarnWithCtx(ctx, "[config reload] Sentinel 限流规则重载失败",
			logger.Err(err),
		)
		return
	}
	logger.InfoWithCtx(ctx, "[config reload] Sentinel 限流规则已更新",
		logger.Int("ruleCount", len(rules)),
		logger.Bool("enableLimit", newCfg.App.EnableLimit),
	)
}

// reloadSentinelBreakerRules Sentinel 熔断规则热更新回调。
// 当 Nacos 配置中的 sentinel.breakerRules 或 app.enableCircuitBreaker 发生变更时，动态重载熔断规则，无需重启服务。
// 规则按开关构建：enableCircuitBreaker=false 时清空规则，彻底关闭熔断；开关本身由 reloadAppFlags 同步切换。
func reloadSentinelBreakerRules(oldCfg, newCfg *Config) {
	ctx := context.Background()
	// 1. 检查熔断规则或开关是否发生变更，均未变更则跳过
	if reflect.DeepEqual(oldCfg.Sentinel.BreakerRules, newCfg.Sentinel.BreakerRules) &&
		oldCfg.App.EnableCircuitBreaker == newCfg.App.EnableCircuitBreaker {
		return
	}

	// 2. 按开关构建规则，开关关闭时用空规则清空全局规则
	var rules []*circuitbreaker.Rule
	if newCfg.App.EnableCircuitBreaker {
		rules = buildBreakerRules(newCfg)
	}

	// 3. circuitbreaker.LoadRules 支持空规则清空（middleware.ReloadBreakerRules 对空规则短路跳过，故直接调用）
	if _, err := circuitbreaker.LoadRules(rules); err != nil {
		logger.WarnWithCtx(ctx, "[config reload] Sentinel 熔断规则重载失败",
			logger.Err(err),
		)
		return
	}
	logger.InfoWithCtx(ctx, "[config reload] Sentinel 熔断规则已更新",
		logger.Int("ruleCount", len(rules)),
		logger.Bool("enableCircuitBreaker", newCfg.App.EnableCircuitBreaker),
	)
}

// buildFlowRules 从配置构建 Sentinel 限流规则列表。
// 将业务配置中的字段映射为 Sentinel SDK 的 flow.Rule 结构体。
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
// 将业务配置中的字段映射为 Sentinel SDK 的 circuitbreaker.Rule 结构体。
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
