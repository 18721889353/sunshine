package logger

import (
	"testing"

	"go.uber.org/zap"
)

// TestGRPCLoggerVerbosity 校验 grpcLogger.V 的详细级别语义（回归 P0-3）。
//
// grpclog/glog 约定：verbosity 表示「允许输出的最大详细级别」，只有 level <= verbosity
// 才启用。历史实现 `verbosity <= level` 方向写反，导致 verbosity=0 时 V(1)/V(2)... 全部
// 返回 true，使 gRPC 内部大量详细日志以 Info 输出、生产刷屏。
func TestGRPCLoggerVerbosity(t *testing.T) {
	t.Run("verbosity=0 仅启用 V(0)", func(t *testing.T) {
		l := &grpcLogger{zLog: zap.NewNop(), verbosity: 0}
		if !l.V(0) {
			t.Error("V(0) 期望 true")
		}
		for _, lvl := range []int{1, 2, 5, 100} {
			if l.V(lvl) {
				t.Errorf("V(%d) 期望 false（verbosity=0 不应放行 gRPC 内部详细日志）", lvl)
			}
		}
	})

	t.Run("verbosity=2 放行 0..2 关闭 3+", func(t *testing.T) {
		l := &grpcLogger{zLog: zap.NewNop(), verbosity: 2}
		for _, lvl := range []int{0, 1, 2} {
			if !l.V(lvl) {
				t.Errorf("verbosity=2 时 V(%d) 期望 true", lvl)
			}
		}
		if l.V(3) {
			t.Error("verbosity=2 时 V(3) 期望 false")
		}
	})
}
