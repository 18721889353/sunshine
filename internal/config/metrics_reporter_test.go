package config

import "testing"

// TestBuildReporterOptions_AllZero 全零阈值 → 不覆盖（走框架默认基线）。
func TestBuildReporterOptions_AllZero(t *testing.T) {
	opts := buildReporterOptions(Metrics{})
	if opts != nil {
		t.Errorf("全零配置不应传 options（应回落框架默认），got %d 个", len(opts))
	}
}

// TestBuildReporterOptions_Partial 至少覆盖一个字段 → 传 options 且阈值正确。
func TestBuildReporterOptions_Partial(t *testing.T) {
	opts := buildReporterOptions(Metrics{P99Warn: 0.5})
	if len(opts) != 1 {
		t.Fatalf("覆盖一个字段应传 1 个 option, got %d", len(opts))
	}
	// options 是闭包无法直接读字段，断言其来源 buildThresholds 的结构
	got, overridden := buildThresholds(Metrics{P99Warn: 0.5})
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
