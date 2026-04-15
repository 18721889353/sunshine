package interceptor

import (
	"context"
	"github.com/18721889353/sunshine/pkg/logger"
	"sync"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/18721889353/sunshine/pkg/krand"
)

var (
	once sync.Once
)

// SetContextRequestIDKey set context request id key (deprecated: use logger.ContextKeyRequestID directly)
// Deprecated: This function is kept for backward compatibility. Use logger.ContextKeyRequestID directly.
func SetContextRequestIDKey(key string) {
	// This function is deprecated and does nothing now
	// All code should use logger.ContextKeyRequestID directly
}

// CtxKeyString for context.WithValue key type
type CtxKeyString string

// RequestIDKey request_id
var RequestIDKey = CtxKeyString(string(logger.ContextKeyRequestID))

// ---------------------------------- client interceptor ----------------------------------

// CtxRequestIDField get request id field from context.Context
func CtxRequestIDField(ctx context.Context) logger.Field {
	return logger.String(string(logger.ContextKeyRequestID), metautils.ExtractOutgoing(ctx).Get(string(logger.ContextKeyRequestID)))
}

// ClientCtxRequestID get request id from rpc client context.Context
func ClientCtxRequestID(ctx context.Context) string {
	return metautils.ExtractOutgoing(ctx).Get(string(logger.ContextKeyRequestID))
}

// ClientCtxRequestIDField get request id field from rpc client context.Context
func ClientCtxRequestIDField(ctx context.Context) logger.Field {
	return logger.String(string(logger.ContextKeyRequestID), metautils.ExtractOutgoing(ctx).Get(string(logger.ContextKeyRequestID)))
}

// UnaryClientRequestID client-side request_id unary interceptor
func UnaryClientRequestID() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		requestID := ClientCtxRequestID(ctx)
		if requestID == "" {
			requestID = krand.String(krand.R_NUM, 32)
			ctx = metadata.AppendToOutgoingContext(ctx, string(logger.ContextKeyRequestID), requestID)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// StreamClientRequestID client request id stream interceptor
func StreamClientRequestID() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
		streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		requestID := ClientCtxRequestID(ctx)
		if requestID == "" {
			requestID = krand.String(krand.R_NUM, 32)
			ctx = metadata.AppendToOutgoingContext(ctx, string(logger.ContextKeyRequestID), requestID)
		}

		return streamer(ctx, desc, cc, method, opts...)
	}
}

// ---------------------------------- server interceptor ----------------------------------

// KV key value
type KV struct {
	Key string
	Val interface{}
}

// WrapServerCtx wrap context, used in grpc server-side
func WrapServerCtx(ctx context.Context, kvs ...KV) context.Context {
	ctx = context.WithValue(ctx, logger.ContextKeyRequestID, metautils.ExtractIncoming(ctx).Get(string(logger.ContextKeyRequestID))) //nolint
	for _, kv := range kvs {
		ctx = context.WithValue(ctx, kv.Key, kv.Val) //nolint
	}
	return ctx
}

// ServerCtxRequestID get request id from rpc server context.Context
func ServerCtxRequestID(ctx context.Context) string {
	return metautils.ExtractIncoming(ctx).Get(string(logger.ContextKeyRequestID))
}

// ServerCtxRequestIDField get request id field from rpc server context.Context
func ServerCtxRequestIDField(ctx context.Context) logger.Field {
	return logger.String(string(logger.ContextKeyRequestID), metautils.ExtractIncoming(ctx).Get(string(logger.ContextKeyRequestID)))
}

// UnaryServerRequestID server-side request_id unary interceptor
func UnaryServerRequestID() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		requestID := ServerCtxRequestID(ctx)
		if requestID == "" {
			requestID = krand.String(krand.R_NUM, 32)
			ctx = metautils.ExtractIncoming(ctx).Add(string(logger.ContextKeyRequestID), requestID).ToIncoming(ctx)
		}

		return handler(ctx, req)
	}
}

// StreamServerRequestID server-side request id stream interceptor
func StreamServerRequestID() grpc.StreamServerInterceptor {
	// todo
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		//ctx := stream.Context()
		//requestID := ServerCtxRequestID(ctx)
		//if requestID == "" {
		//	requestID = krand.String(krand.R_NUM, 32)
		//	ctx = metautils.ExtractIncoming(ctx).Add(ContextRequestIDKey, requestID).ToIncoming(ctx)
		//}
		return handler(srv, stream)
	}
}
