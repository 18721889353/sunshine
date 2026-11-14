package middleware

import (
	"strings"
	"testing"

	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
)

// TestSentinelBlockReasonFlow 校验限流拦截的人话描述包含「阈值/窗口/当前值」关键信息。
func TestSentinelBlockReasonFlow(t *testing.T) {
	rule := &flow.Rule{
		Resource:         "/Api/Audiovisual/Order/Recharge",
		Threshold:        200,
		StatIntervalInMs: 1000,
	}
	blockErr := base.NewBlockErrorWithCause(base.BlockTypeFlow, "flow reject check blocked", rule, float64(200))
	reason := sentinelBlockReason(blockErr)
	t.Logf("flow reason: %s", reason)

	for _, want := range []string{"触发限流", "/Api/Audiovisual/Order/Recharge", "200", "1000", "阈值"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("限流描述缺少 %q，实际: %s", want, reason)
		}
	}
}

// TestSentinelBlockReasonCircuitBreaking 校验熔断拦截的人话描述包含「策略/阈值/恢复时长」关键信息。
func TestSentinelBlockReasonCircuitBreaking(t *testing.T) {
	rule := &circuitbreaker.Rule{
		Resource:         "/Api/Order/Order/placeOrder",
		Strategy:         circuitbreaker.ErrorRatio,
		Threshold:        0.3,
		StatIntervalMs:   20000,
		MinRequestAmount: 200,
		RetryTimeoutMs:   5000,
	}
	blockErr := base.NewBlockErrorWithCause(base.BlockTypeCircuitBreaking, "circuit breaker check blocked", rule, nil)
	reason := sentinelBlockReason(blockErr)
	t.Logf("breaker reason: %s", reason)

	for _, want := range []string{"触发熔断", "/Api/Order/Order/placeOrder", "ErrorRatio", "0.30", "5000"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("熔断描述缺少 %q，实际: %s", want, reason)
		}
	}
}

// TestSentinelBlockReasonNil 保证 nil/未知 blockType 不 panic 且有兜底文案。
func TestSentinelBlockReasonNil(t *testing.T) {
	if got := sentinelBlockReason(nil); got == "" {
		t.Fatal("nil BlockError 应返回非空兜底描述")
	}
	unknown := base.NewBlockErrorWithMessage(base.BlockTypeUnknown, "custom msg")
	if got := sentinelBlockReason(unknown); !strings.Contains(got, "custom msg") {
		t.Fatalf("未知类型应回退到 BlockMsg，实际: %s", got)
	}
}
