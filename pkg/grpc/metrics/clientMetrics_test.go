package metrics

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/stats"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
	"github.com/18721889353/sunshine/pkg/utils"
)

// resetClientConnCount 测试隔离：重置包级连接计数与 gauge。
func resetClientConnCount(t *testing.T) {
	t.Helper()
	clientConnCount.Store(0)
	grpcClientConnectionGauge.Set(0)
}

// clientConnGaugeValue 读取客户端活跃连接 gauge 当前值。
func clientConnGaugeValue(t *testing.T) float64 {
	t.Helper()
	return testutil.ToFloat64(grpcClientConnectionGauge)
}

func TestClientHTTPService(t *testing.T) {
	serverAddr, _ := utils.GetLocalHTTPAddrPairs()

	s := ClientHTTPService(serverAddr)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	time.Sleep(time.Millisecond * 100)
	err := s.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestStreamClientMetrics(t *testing.T) {
	metrics := StreamClientMetrics()
	assert.NotNil(t, metrics)
}

func TestUnaryClientMetrics(t *testing.T) {
	metrics := UnaryClientMetrics()
	assert.NotNil(t, metrics)
}

func Test_cliRegisterMetrics(t *testing.T) {
	cliRegisterMetrics()
}

func TestClientRegister(t *testing.T) {
	SetClientPattern("/rpc_client/metrics")
	ClientRegister(http.NewServeMux())
}

// TestClientConnCountingHandler 连接计数 stats.Handler：
// ConnBegin → gauge+1、ConnEnd → gauge-1（与 server 侧 CustomListener 对称）。
func TestClientConnCountingHandler(t *testing.T) {
	resetClientConnCount(t)
	h := NewClientConnCountingHandler()
	ctx := context.Background()

	before := clientConnGaugeValue(t)

	h.HandleConn(ctx, &stats.ConnBegin{})
	h.HandleConn(ctx, &stats.ConnBegin{})
	if got := clientConnGaugeValue(t); got != before+2 {
		t.Errorf("两次 ConnBegin 后应 +2, before=%v got=%v", before, got)
	}

	h.HandleConn(ctx, &stats.ConnEnd{})
	if got := clientConnGaugeValue(t); got != before+1 {
		t.Errorf("一次 ConnEnd 后应 +1, before=%v got=%v", before, got)
	}

	// 边界：重复 ConnEnd 不应让 gauge 变负（gRPC 多关闭路径兜底）
	h.HandleConn(ctx, &stats.ConnEnd{})
	h.HandleConn(ctx, &stats.ConnEnd{})
	if got := clientConnGaugeValue(t); got != 0 {
		t.Errorf("重复 ConnEnd 后应为 0（clamp）, got %v", got)
	}

	// TagRPC/TagConn 应透传（不 panic、不丢 ctx）
	if got := h.TagRPC(ctx, &stats.RPCTagInfo{FullMethodName: "/x"}); got != ctx {
		t.Error("TagRPC 应返回原 ctx")
	}
	if got := h.TagConn(ctx, &stats.ConnTagInfo{}); got != ctx {
		t.Error("TagConn 应返回原 ctx")
	}
}

// TestClientActiveConnectionsRegistered 指标进统一注册中心（巡检可达 + componentSpecs 命中）。
func TestClientActiveConnectionsRegistered(t *testing.T) {
	cliRegisterMetrics()
	if !pkgmetrics.Default().HasMetric("grpc_client_active_connections") {
		t.Error("grpc_client_active_connections 应注册进统一中心")
	}
}

// TestClientConnCountingHandler_Concurrent 并发 Begin/End 后计数归零（原子 CAS 正确性）。
func TestClientConnCountingHandler_Concurrent(t *testing.T) {
	resetClientConnCount(t)
	h := NewClientConnCountingHandler()
	ctx := context.Background()
	const n = 100

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			h.HandleConn(ctx, &stats.ConnBegin{})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			h.HandleConn(ctx, &stats.ConnEnd{})
		}
	}()
	wg.Wait()

	// End 先跑时 clamp 到 0，Begin 后跑会 +1 → 最终介于 0 和 n 之间，绝不为负。
	got := testutil.ToFloat64(grpcClientConnectionGauge)
	if got < 0 || got > n {
		t.Errorf("并发 Begin/End 后计数应在 [0,%d], got %v", n, got)
	}
}

// TestClientConnCountingHandler_MultipleInstances 多 ClientConn 各 new 一个 handler，
// 共享同一包级计数（实例级计数会互相覆盖全局 gauge——已修复的缺陷）。
func TestClientConnCountingHandler_MultipleInstances(t *testing.T) {
	resetClientConnCount(t)
	ctx := context.Background()
	h1 := NewClientConnCountingHandler()
	h2 := NewClientConnCountingHandler()

	h1.HandleConn(ctx, &stats.ConnBegin{})
	h2.HandleConn(ctx, &stats.ConnBegin{})
	if got := clientConnGaugeValue(t); got != 2 {
		t.Errorf("两个 handler 各 Begin 一次应累计 2, got %v", got)
	}

	h1.HandleConn(ctx, &stats.ConnEnd{})
	if got := clientConnGaugeValue(t); got != 1 {
		t.Errorf("h1 End 后应为 1, got %v", got)
	}
}
