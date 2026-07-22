package interceptor

import (
	"context"
	"testing"

	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestParseTokenCalculateStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected flow.TokenCalculateStrategy
	}{
		{"direct", flow.Direct},
		{"warmUp", flow.WarmUp},
		{"unknown", flow.Direct},
		{"", flow.Direct},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseTokenCalculateStrategy(tt.input))
		})
	}
}

func TestParseControlBehavior(t *testing.T) {
	tests := []struct {
		input    string
		expected flow.ControlBehavior
	}{
		{"reject", flow.Reject},
		{"throttling", flow.Throttling},
		{"unknown", flow.Reject},
		{"", flow.Reject},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseControlBehavior(tt.input))
		})
	}
}

func TestParseBreakerStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected circuitbreaker.Strategy
	}{
		{"errorRatio", circuitbreaker.ErrorRatio},
		{"errorCount", circuitbreaker.ErrorCount},
		{"slowRequestRatio", circuitbreaker.SlowRequestRatio},
		{"unknown", circuitbreaker.ErrorRatio},
		{"", circuitbreaker.ErrorRatio},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseBreakerStrategy(tt.input))
		})
	}
}

func TestUnaryServerRateLimit(t *testing.T) {
	interceptor := UnaryServerRateLimit(
		WithSentinelFlowRules([]*flow.Rule{
			{
				Resource:               "/test.Test/Unary",
				TokenCalculateStrategy: flow.Direct,
				ControlBehavior:        flow.Reject,
				Threshold:              10000,
				StatIntervalInMs:       1000,
			},
		}),
	)
	assert.NotNil(t, interceptor)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	}
	_, err := interceptor(nil, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Test/Unary"}, handler)
	assert.NoError(t, err)
}

func TestStreamServerRateLimit(t *testing.T) {
	interceptor := StreamServerRateLimit(
		WithSentinelFlowRules([]*flow.Rule{
			{
				Resource:               "/test.Test/Stream",
				TokenCalculateStrategy: flow.Direct,
				ControlBehavior:        flow.Reject,
				Threshold:              10000,
				StatIntervalInMs:       1000,
			},
		}),
	)
	assert.NotNil(t, interceptor)

	handler := func(srv interface{}, stream grpc.ServerStream) error {
		return nil
	}
	err := interceptor(nil, nil, &grpc.StreamServerInfo{FullMethod: "/test.Test/Stream"}, handler)
	assert.NoError(t, err)
}

func TestUnaryServerCircuitBreaker(t *testing.T) {
	interceptor := UnaryServerCircuitBreaker(
		WithSentinelCircuitBreakerRules([]*circuitbreaker.Rule{
			{
				Resource:         "/test.Test/UnaryCB",
				Strategy:         circuitbreaker.ErrorRatio,
				RetryTimeoutMs:   10000,
				MinRequestAmount: 3,
				StatIntervalMs:   10000,
				Threshold:        0.5,
			},
		}),
	)
	assert.NotNil(t, interceptor)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	}
	_, err := interceptor(nil, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Test/UnaryCB"}, handler)
	assert.NoError(t, err)
}

func TestStreamServerCircuitBreaker(t *testing.T) {
	interceptor := StreamServerCircuitBreaker(
		WithSentinelCircuitBreakerRules([]*circuitbreaker.Rule{
			{
				Resource:         "/test.Test/StreamCB",
				Strategy:         circuitbreaker.ErrorRatio,
				RetryTimeoutMs:   10000,
				MinRequestAmount: 3,
				StatIntervalMs:   10000,
				Threshold:        0.5,
			},
		}),
	)
	assert.NotNil(t, interceptor)

	handler := func(srv interface{}, stream grpc.ServerStream) error {
		return nil
	}
	err := interceptor(nil, nil, &grpc.StreamServerInfo{FullMethod: "/test.Test/StreamCB"}, handler)
	assert.NoError(t, err)
}

func TestUnaryClientCircuitBreaker(t *testing.T) {
	interceptor := UnaryClientCircuitBreaker(
		WithSentinelCircuitBreakerRules([]*circuitbreaker.Rule{
			{
				Resource:         "/test.Test/ClientCB",
				Strategy:         circuitbreaker.ErrorRatio,
				RetryTimeoutMs:   10000,
				MinRequestAmount: 3,
				StatIntervalMs:   10000,
				Threshold:        0.5,
			},
		}),
	)
	assert.NotNil(t, interceptor)

	// 模拟一个成功的 gRPC 调用
	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return nil
	}
	err := interceptor(nil, "test", nil, nil, nil, invoker)
	assert.NoError(t, err)
}

func TestStreamClientCircuitBreaker(t *testing.T) {
	interceptor := StreamClientCircuitBreaker(
		WithSentinelCircuitBreakerRules([]*circuitbreaker.Rule{
			{
				Resource:         "/test.Test/StreamClientCB",
				Strategy:         circuitbreaker.ErrorRatio,
				RetryTimeoutMs:   10000,
				MinRequestAmount: 3,
				StatIntervalMs:   10000,
				Threshold:        0.5,
			},
		}),
	)
	assert.NotNil(t, interceptor)

	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return nil, nil
	}
	_, err := interceptor(nil, nil, nil, "test", streamer)
	assert.NoError(t, err)
}

func TestErrNotAllowed(t *testing.T) {
	assert.Equal(t, codes.Unavailable, status.Code(ErrNotAllowed))
}

func TestErrLimitExceed(t *testing.T) {
	assert.Equal(t, codes.ResourceExhausted, status.Code(ErrLimitExceed))
}

func TestCircuitBreakerSetErrorOnInternal(t *testing.T) {
	interceptor := UnaryServerCircuitBreaker(
		WithSentinelCircuitBreakerRules([]*circuitbreaker.Rule{
			{
				Resource:         "/test.Test/CBError",
				Strategy:         circuitbreaker.ErrorRatio,
				RetryTimeoutMs:   10000,
				MinRequestAmount: 1,
				StatIntervalMs:   10000,
				Threshold:        0.1,
			},
		}),
	)
	assert.NotNil(t, interceptor)

	// handler 返回 Internal 错误，验证拦截器正确处理
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, status.Error(codes.Internal, "internal error")
	}
	_, err := interceptor(nil, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Test/CBError"}, handler)
	assert.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestCircuitBreakerIgnoreNonInternalError(t *testing.T) {
	interceptor := UnaryServerCircuitBreaker(
		WithSentinelCircuitBreakerRules([]*circuitbreaker.Rule{
			{
				Resource:         "/test.Test/CBIgnore",
				Strategy:         circuitbreaker.ErrorRatio,
				RetryTimeoutMs:   10000,
				MinRequestAmount: 1,
				StatIntervalMs:   10000,
				Threshold:        0.5,
			},
		}),
	)
	assert.NotNil(t, interceptor)

	// handler 返回 NotFound 错误，不应触发熔断
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, status.Error(codes.NotFound, "not found")
	}
	_, err := interceptor(nil, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Test/CBIgnore"}, handler)
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}
