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
// 当 Nacos 配置中的 sentinel.limitRules 发生变更时，动态重载限流规则，无需重启服务。
// 注意：仅当 EnableLimit=true 时生效，否则跳过更新。
func reloadSentinelFlowRules(oldCfg, newCfg *Config) {
	ctx := context.Background()
	// 1. 检查限流规则是否发生变更，未变更则跳过
	if reflect.DeepEqual(oldCfg.Sentinel.LimitRules, newCfg.Sentinel.LimitRules) {
		return
	}
	// 2. 功能开关未开启时跳过更新
	if !newCfg.App.EnableLimit {
		return
	}

	rules := buildFlowRules(newCfg)
	if err := middleware.ReloadFlowRules(rules); err != nil {
		logger.WarnWithCtx(ctx, "[config reload] Sentinel 限流规则重载失败",
			logger.Err(err),
		)
		return
	}
	logger.InfoWithCtx(ctx, "[config reload] Sentinel 限流规则已更新",
		logger.Int("ruleCount", len(rules)),
	)
}

// reloadSentinelBreakerRules Sentinel 熔断规则热更新回调。
// 当 Nacos 配置中的 sentinel.breakerRules 发生变更时，动态重载熔断规则，无需重启服务。
// 注意：仅当 EnableCircuitBreaker=true 时生效，否则跳过更新。
func reloadSentinelBreakerRules(oldCfg, newCfg *Config) {
	ctx := context.Background()
	// 1. 检查熔断规则是否发生变更，未变更则跳过
	if reflect.DeepEqual(oldCfg.Sentinel.BreakerRules, newCfg.Sentinel.BreakerRules) {
		return
	}
	// 2. 功能开关未开启时跳过更新
	if !newCfg.App.EnableCircuitBreaker {
		return
	}

	rules := buildBreakerRules(newCfg)
	if err := middleware.ReloadBreakerRules(rules); err != nil {
		logger.WarnWithCtx(ctx, "[config reload] Sentinel 熔断规则重载失败",
			logger.Err(err),
		)
		return
	}
	logger.InfoWithCtx(ctx, "[config reload] Sentinel 熔断规则已更新",
		logger.Int("ruleCount", len(rules)),
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
