package interceptor

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// ---------------------------------- client interceptor ----------------------------------

var timeoutVal = time.Second * 10

// UnaryClientTimeout client-side timeout unary interceptor
func UnaryClientTimeout(d time.Duration) grpc.UnaryClientInterceptor {
	if d < time.Millisecond {
		d = timeoutVal
	}

	return func(ctx context.Context, method string, req, resp interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		err := invoker(ctx, method, req, resp, cc, opts...)
		// 超时是系统劣化的早期信号——必须可见（server 可能返回 DeadlineExceeded，
		// 也可能 ctx 先到期，两种都覆盖）
		if status.Code(err) == codes.DeadlineExceeded || ctx.Err() == context.DeadlineExceeded {
			pkgmetrics.ObserveTimeout("grpc-unary-client")
		}
		return err
	}
}

// StreamClientTimeout server-side timeout  interceptor
func StreamClientTimeout(d time.Duration) grpc.StreamClientInterceptor {
	if d < time.Millisecond {
		d = timeoutVal
	}

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		stream, err := streamer(ctx, desc, cc, method, opts...)
		if status.Code(err) == codes.DeadlineExceeded || ctx.Err() == context.DeadlineExceeded {
			pkgmetrics.ObserveTimeout("grpc-stream-client")
		}
		return stream, err
	}
}
