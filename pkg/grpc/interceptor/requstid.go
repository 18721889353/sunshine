package interceptor

import (
	"context"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/18721889353/sunshine/pkg/krand"
)

// SetContextRequestIDKey 设置上下文 request_id 的键（已废弃）
// Deprecated: 此函数仅为向后兼容而保留，请直接使用 logger.ContextKeyRequestID
func SetContextRequestIDKey(_ string) {
	// 此函数已废弃，不再执行任何操作
	// 所有代码应直接使用 logger.ContextKeyRequestID
}

// CtxKeyString 用于 context.WithValue 的键类型
type CtxKeyString string

// RequestIDKey request_id 的上下文键
var RequestIDKey = CtxKeyString(string(logger.ContextKeyRequestID))

// ---------------------------------- 客户端拦截器 ----------------------------------

// CtxRequestIDField 从 context.Context 中获取 request_id 字段（用于日志记录）
// 参数:
//   - ctx: 上下文对象
//
// 返回:
//   - logger.Field: 包含 request_id 的日志字段
func CtxRequestIDField(ctx context.Context) logger.Field {
	return logger.String(string(logger.ContextKeyRequestID), metautils.ExtractOutgoing(ctx).Get(string(logger.ContextKeyRequestID)))
}

// ClientCtxRequestID 从 gRPC 客户端 context.Context 中获取 request_id
// 参数:
//   - ctx: 上下文对象
//
// 返回:
//   - string: request_id 字符串，如果不存在则返回空字符串
func ClientCtxRequestID(ctx context.Context) string {
	return metautils.ExtractOutgoing(ctx).Get(string(logger.ContextKeyRequestID))
}

// ClientCtxRequestIDField 从 gRPC 客户端 context.Context 中获取 request_id 字段（用于日志记录）
// 参数:
//   - ctx: 上下文对象
//
// 返回:
//   - logger.Field: 包含 request_id 的日志字段
func ClientCtxRequestIDField(ctx context.Context) logger.Field {
	return logger.String(string(logger.ContextKeyRequestID), metautils.ExtractOutgoing(ctx).Get(string(logger.ContextKeyRequestID)))
}

// UnaryClientRequestID gRPC 客户端一元请求 request_id 拦截器
// 功能：
// 1. 检查 outgoing metadata 中是否存在 request_id
// 2. 如果不存在，生成一个新的 request_id 并添加到 outgoing metadata
// 3. 确保客户端发出的每个请求都携带 request_id，便于全链路追踪
//
// 使用场景：
// - gRPC 客户端发起一元调用时自动注入 request_id
// - 与 UnaryServerRequestID 配合使用，实现完整的请求追踪
//
// 返回:
//   - grpc.UnaryClientInterceptor: 一元客户端拦截器
func UnaryClientRequestID() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		requestID := ClientCtxRequestID(ctx)
		if requestID == "" {
			// 如果 request_id 不存在，生成一个 32 位随机数字符串
			requestID = krand.String(krand.R_NUM, 32)
			ctx = metadata.AppendToOutgoingContext(ctx, string(logger.ContextKeyRequestID), requestID)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// StreamClientRequestID gRPC 客户端流式请求 request_id 拦截器
// 功能：
// 1. 检查 outgoing metadata 中是否存在 request_id
// 2. 如果不存在，生成一个新的 request_id 并添加到 outgoing metadata
// 3. 确保客户端发出的每个流式请求都携带 request_id
//
// 使用场景：
// - gRPC 客户端发起流式调用时自动注入 request_id
// - 与 StreamServerRequestID 配合使用，实现流式请求的追踪
//
// 返回:
//   - grpc.StreamClientInterceptor: 流式客户端拦截器
func StreamClientRequestID() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
		streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		requestID := ClientCtxRequestID(ctx)
		if requestID == "" {
			// 如果 request_id 不存在，生成一个 32 位随机数字符串
			requestID = krand.String(krand.R_NUM, 32)
			ctx = metadata.AppendToOutgoingContext(ctx, string(logger.ContextKeyRequestID), requestID)
		}

		return streamer(ctx, desc, cc, method, opts...)
	}
}

// ---------------------------------- 服务端拦截器 ----------------------------------

// KV 键值对结构体，用于在 WrapServerCtx 中传递自定义的上下文数据
type KV struct {
	Key string      // 上下文的键
	Val interface{} // 上下文的值
}

// WrapServerCtx 包装 gRPC 服务端上下文，主要用于：
// 1. 从 gRPC incoming metadata 中提取 request_id，并设置到 context 中
// 2. 支持通过可变参数 kvs 添加额外的自定义键值对到 context
// 3. 确保后续的日志记录、链路追踪等功能能够正确获取 request_id
//
// 使用场景：
// - gRPC Server 端拦截器中，标准化上下文信息
// - HTTP 到 gRPC 的桥接场景中，传递请求元数据
// - 需要在多个服务调用之间保持上下文一致性的场景
//
// 注意：
//   - 该函数会从 incoming metadata 中提取 request_id，如果不存在则为空字符串
//   - 如果在非 gRPC 场景（如 RabbitMQ、Kafka 消费者）中使用，incoming metadata 为空
//     会导致提取的 request_id 为空，可能覆盖之前设置的值
//   - 在非 gRPC 场景中，建议直接使用 context.WithValue 设置 request_id
func WrapServerCtx(ctx context.Context, kvs ...KV) context.Context {
	// 从 gRPC incoming metadata 中提取 request_id 并设置到 context
	// 如果 metadata 中不存在 request_id，则返回空字符串
	ctx = context.WithValue(ctx, logger.ContextKeyRequestID, metautils.ExtractIncoming(ctx).Get(string(logger.ContextKeyRequestID))) //nolint

	// 遍历并添加所有自定义的键值对到 context
	for _, kv := range kvs {
		ctx = context.WithValue(ctx, kv.Key, kv.Val) //nolint
	}
	return ctx
}

// ServerCtxRequestID 从 gRPC 服务端 context.Context 中获取 request_id
// 参数:
//   - ctx: 上下文对象
//
// 返回:
//   - string: request_id 字符串，从 incoming metadata 中提取，如果不存在则返回空字符串
func ServerCtxRequestID(ctx context.Context) string {
	return metautils.ExtractIncoming(ctx).Get(string(logger.ContextKeyRequestID))
}

// ServerCtxRequestIDField 从 gRPC 服务端 context.Context 中获取 request_id 字段（用于日志记录）
// 参数:
//   - ctx: 上下文对象
//
// 返回:
//   - logger.Field: 包含 request_id 的日志字段
func ServerCtxRequestIDField(ctx context.Context) logger.Field {
	return logger.String(string(logger.ContextKeyRequestID), metautils.ExtractIncoming(ctx).Get(string(logger.ContextKeyRequestID)))
}

// UnaryServerRequestID gRPC 服务端一元请求 request_id 拦截器
// 功能：
// 1. 从 incoming metadata 中提取 request_id
// 2. 如果不存在，生成一个新的 request_id 并添加到 incoming metadata
// 3. 确保服务端处理的每个请求都有 request_id，便于日志记录和链路追踪
//
// 使用场景：
// - gRPC 服务端接收一元请求时自动提取或生成 request_id
// - 与 UnaryClientRequestID 配合使用，实现完整的请求追踪
// - 日志系统可以通过 ServerCtxRequestIDField 自动记录 request_id
//
// 返回:
//   - grpc.UnaryServerInterceptor: 一元服务端拦截器
func UnaryServerRequestID() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		requestID := ServerCtxRequestID(ctx)
		if requestID == "" {
			// 如果 request_id 不存在，生成一个 32 位随机数字符串
			requestID = krand.String(krand.R_NUM, 32)
			ctx = metautils.ExtractIncoming(ctx).Add(string(logger.ContextKeyRequestID), requestID).ToIncoming(ctx)
		}

		return handler(ctx, req)
	}
}

// StreamServerRequestID gRPC 服务端流式请求 request_id 拦截器（待实现）
// TODO: 实现流式请求的 request_id 提取和注入逻辑
//
// 预期功能：
// 1. 从 stream context 的 incoming metadata 中提取 request_id
// 2. 如果不存在，生成一个新的 request_id
// 3. 确保流式请求也能正确追踪
//
// 返回:
//   - grpc.StreamServerInterceptor: 流式服务端拦截器
func StreamServerRequestID() grpc.StreamServerInterceptor {
	// todo
	return func(srv interface{}, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		//ctx := stream.Context()
		//requestID := ServerCtxRequestID(ctx)
		//if requestID == "" {
		//	requestID = krand.String(krand.R_NUM, 32)
		//	ctx = metautils.ExtractIncoming(ctx).Add(ContextRequestIDKey, requestID).ToIncoming(ctx)
		//}
		return handler(srv, stream)
	}
}
