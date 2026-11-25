package interceptor

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc/stats"
)

// NewClientStatsHandler returns a new client-side stats handler for tracing
func NewClientStatsHandler() stats.Handler {
	return otelgrpc.NewClientHandler()
}

// NewServerStatsHandler returns a new server-side stats handler for tracing
func NewServerStatsHandler() stats.Handler {
	return otelgrpc.NewServerHandler()
}
