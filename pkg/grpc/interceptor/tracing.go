package interceptor

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
)

// UnaryClientTracing client-side tracing unary interceptor
// Deprecated: Use NewClientStatsHandler instead
func UnaryClientTracing() grpc.UnaryClientInterceptor {
	return nil // StatsHandler is used instead of interceptors in v0.62.0+
}

// StreamClientTracing client-side tracing stream interceptor
// Deprecated: Use NewClientStatsHandler instead
func StreamClientTracing() grpc.StreamClientInterceptor {
	return nil // StatsHandler is used instead of interceptors in v0.62.0+
}

// UnaryServerTracing server-side tracing unary interceptor
// Deprecated: Use NewServerStatsHandler instead
func UnaryServerTracing() grpc.UnaryServerInterceptor {
	return nil // StatsHandler is used instead of interceptors in v0.62.0+
}

// StreamServerTracing server-side tracing stream interceptor
// Deprecated: Use NewServerStatsHandler instead
func StreamServerTracing() grpc.StreamServerInterceptor {
	return nil // StatsHandler is used instead of interceptors in v0.62.0+
}

// NewClientStatsHandler returns a new client-side stats handler for tracing
func NewClientStatsHandler() stats.Handler {
	return otelgrpc.NewClientHandler()
}

// NewServerStatsHandler returns a new server-side stats handler for tracing
func NewServerStatsHandler() stats.Handler {
	return otelgrpc.NewServerHandler()
}