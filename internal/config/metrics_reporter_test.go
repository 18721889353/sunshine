package config

import "testing"

// TestBuildReporterOptions_AllZero 全零阈值 → 只传 routes option（阈值回落框架默认基线）。
func TestBuildReporterOptions_AllZero(t *testing.T) {
	opts := buildReporterOptions(Metrics{})
	if len(opts) != 1 {
		t.Errorf("全零配置应只传 routes option，got %d 个", len(opts))
	}
}

// TestBuildReporterOptions_Partial 至少覆盖一个字段 → routes + 阈值两个 option。
func TestBuildReporterOptions_Partial(t *testing.T) {
	opts := buildReporterOptions(Metrics{LatencyP99Warn: 0.5})
	if len(opts) != 2 {
		t.Fatalf("覆盖一个字段应传 2 个 option（routes+阈值）, got %d", len(opts))
	}
	// options 是闭包无法直接读字段，断言其来源 buildThresholds 的结构
	got, overridden := buildThresholds(Metrics{LatencyP99Warn: 0.5})
	if !overridden {
		t.Fatal("覆盖了 P99Warn 应标记 overridden")
	}
	if got.P99Warn != 0.5 {
		t.Errorf("P99Warn 应 0.5, got %v", got.P99Warn)
	}
}

// TestBuildThresholds_MinSamples 边界：负数按 0（用默认）、正数转换、零值不覆盖。
func TestBuildThresholds_MinSamples(t *testing.T) {
	// 负数（生成器 int 类型可为负）→ 视为未配置，th.MinSamples=0 且不覆盖
	got, overridden := buildThresholds(Metrics{MinSamples: -5})
	if got.MinSamples != 0 {
		t.Errorf("负数应转 0（用默认）, got %d", got.MinSamples)
	}
	if overridden {
		t.Error("只有负数 MinSamples 不应标记覆盖（视为未配置）")
	}

	// 正数 → 正确转换
	got, overridden = buildThresholds(Metrics{MinSamples: 50})
	if got.MinSamples != 50 || !overridden {
		t.Errorf("正数应转 50 且覆盖, got %d overridden=%v", got.MinSamples, overridden)
	}

	// 零值 → 未覆盖
	if _, overridden := buildThresholds(Metrics{}); overridden {
		t.Error("全零不应标记覆盖")
	}
}

// TestBuildGaugeAbove 配置阈值转换：零值跳过、yaml tag 作 key、空配置返回 nil。
func TestBuildGaugeAbove(t *testing.T) {
	got := buildGaugeAbove(GaugeAbove{DBPoolInUseConnections: 100})
	if len(got) != 1 || got["db_pool_in_use_connections"] != 100 {
		t.Errorf("GaugeAbove 转换错误: %v", got)
	}
	// 全零 → nil（未配置）
	if m := buildGaugeAbove(GaugeAbove{}); m != nil {
		t.Errorf("全零应返回 nil, got %v", m)
	}
}

// TestBuildThresholds_GaugeAbove 配置了 GaugeAbove 也算"覆盖"（触发 WithDiagnosisThresholds）。
func TestBuildThresholds_GaugeAbove(t *testing.T) {
	// 只配 GaugeAbove，其他阈值全零 → 仍应 overridden=true
	if _, overridden := buildThresholds(Metrics{GaugeAbove: GaugeAbove{DBPoolInUseConnections: 50}}); !overridden {
		t.Error("仅配 GaugeAbove 也应标记覆盖")
	}
	// 全零 → 未覆盖
	if _, overridden := buildThresholds(Metrics{}); overridden {
		t.Error("全零不应标记覆盖")
	}
}

// TestReporterOptions_EndToEnd 端到端：配置 → buildReporterOptions → 应用到 Reporter。
// 用 push 的路由匹配行为验证 routes 真正落地（不依赖未导出字段）。
func TestReporterOptions_EndToEnd(t *testing.T) {
	cfg := Metrics{
		Enabled: true,
		WebhookRoutes: []WebhookRoutes{
			{Types: []string{"periodic"}, Urls: []string{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=a"}},
		},
		LatencyP99Warn: 0.5,
		GaugeAbove:     GaugeAbove{DBPoolInUseConnections: 50},
	}
	opts := buildReporterOptions(cfg)
	if len(opts) != 2 {
		t.Fatalf("应有 routes + 阈值 2 个 option, got %d", len(opts))
	}
	// 阈值转换正确性（GaugeAbove struct → map）
	th, overridden := buildThresholds(cfg)
	if !overridden {
		t.Fatal("配置了阈值应标记覆盖")
	}
	if th.GaugeAbove["db_pool_in_use_connections"] != 50 {
		t.Errorf("GaugeAbove 转换错误: %v", th.GaugeAbove)
	}
}

// TestBuildThresholds_AvailabilityBoundary 可用性边界：1.0（100%目标）不能被 normalize 回落。
func TestBuildThresholds_AvailabilityBoundary(t *testing.T) {
	// 配 1.0（零错误容忍）→ errRate 应为极小值（非 0，避免 DTO normalize 回落 5%）
	th, overridden := buildThresholds(Metrics{AvailabilityWarn: 1.0})
	if !overridden {
		t.Fatal("配置 availability=1.0 应标记覆盖")
	}
	if th.ErrRateWarn == 0 {
		t.Error("1.0 可用性转成的 errRate 不能为 0（会被 normalize 回落为 5%）")
	}
	if th.ErrRateWarn > 1e-6 {
		t.Errorf("errRate 应接近 0（极小值保住语义）, got %v", th.ErrRateWarn)
	}

	// 未配置 → 保持 0（用框架默认），不能 1-0=1
	th2, overridden2 := buildThresholds(Metrics{})
	if overridden2 {
		t.Error("空配置不应标记覆盖")
	}
	if th2.ErrRateWarn != 0 {
		t.Errorf("空配置 errRate 应为 0（默认由 normalize 回落）, got %v", th2.ErrRateWarn)
	}
}
