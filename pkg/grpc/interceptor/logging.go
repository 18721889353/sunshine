package interceptor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/18721889353/sunshine/pkg/logger"
)

// ---------------------------------- client interceptor ----------------------------------

// UnaryClientLog client log unary interceptor
func UnaryClientLog(opts ...LogOption) grpc.UnaryClientInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)
	if o.isReplaceGRPCLogger {
		logger.ReplaceGRPCLoggerV2(logger.Get())
	}

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		startTime := time.Now()

		fields := []logger.Field{
			logger.String("type", "unary"),
			logger.String("method", method),
			logger.Any("request", req),
		}
		if requestID := ClientCtxRequestID(ctx); requestID != "" {
			fields = append(fields, logger.String(string(logger.ContextKeyRequestID), requestID))
		}
		fields = append(fields, logger.String("log_from", o.logFrom+" invoker request UnaryClientLog"))
		logger.InfoWithCtx(ctx, "invoker request", fields...)

		err := invoker(ctx, method, req, reply, cc, opts...)

		fields = []logger.Field{
			logger.String("code", status.Code(err).String()),
			logger.String("type", "unary"),
			logger.String("method", method),
			logger.Any("reply", reply),
			logger.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if err != nil {
			fields = append(fields, logger.Err(err))
		}
		if requestID := ClientCtxRequestID(ctx); requestID != "" {
			fields = append(fields, logger.String(string(logger.ContextKeyRequestID), requestID))
		}
		fields = append(fields, logger.String("log_from", o.logFrom+" invoker result UnaryClientLog"))
		logger.InfoWithCtx(ctx, "invoker result", fields...)
		return err
	}
}

// StreamClientLog client log stream interceptor
func StreamClientLog(opts ...LogOption) grpc.StreamClientInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)
	if o.isReplaceGRPCLogger {
		logger.ReplaceGRPCLoggerV2(logger.Get())
	}

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
		streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		startTime := time.Now()

		clientStream, err := streamer(ctx, desc, cc, method, opts...)

		fields := []logger.Field{
			logger.String("code", status.Code(err).String()),
			logger.String("type", "stream"),
			logger.String("method", method),
			logger.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if err != nil {
			fields = append(fields, logger.Err(err))
		}
		if requestID := ClientCtxRequestID(ctx); requestID != "" {
			fields = append(fields, logger.String(string(logger.ContextKeyRequestID), requestID))
		}
		fields = append(fields, logger.String("log_from", "gw StreamClientLog"))
		logger.InfoWithCtx(ctx, "invoker result", fields...)

		return clientStream, err
	}
}

// ---------------------------------- server interceptor ----------------------------------

// LogOption log settings
type LogOption func(*logOptions)

type logOptions struct {
	fields              map[string]interface{}
	ignoreMethods       map[string]struct{}
	isReplaceGRPCLogger bool
	maxLength           int
	logFrom             string
}

func defaultLogOptions() *logOptions {
	return &logOptions{
		fields:        make(map[string]interface{}),
		ignoreMethods: make(map[string]struct{}),
		maxLength:     300,
		logFrom:       "",
	}
}

func (o *logOptions) apply(opts ...LogOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithMaxLen 设置日志最大长度
func WithMaxLen(maxLen int) LogOption {
	return func(o *logOptions) {
		o.maxLength = maxLen
	}
}

// WithLogFrom logger logFrom
func WithLogFrom(logFrom string) LogOption {
	return func(o *logOptions) {
		o.logFrom = logFrom
	}
}

// WithReplaceGRPCLogger replace grpc logger v2
func WithReplaceGRPCLogger() LogOption {
	return func(o *logOptions) {
		o.isReplaceGRPCLogger = true
	}
}

// WithLogFields adding a custom print field
func WithLogFields(kvs map[string]interface{}) LogOption {
	return func(o *logOptions) {
		if len(kvs) == 0 {
			return
		}
		o.fields = kvs
	}
}

// WithLogIgnoreMethods ignore printing methods
// fullMethodName format: /packageName.serviceName/methodName,
// example /api.userExample.v1.userExampleService/GetByID
func WithLogIgnoreMethods(fullMethodNames ...string) LogOption {
	return func(o *logOptions) {
		for _, method := range fullMethodNames {
			o.ignoreMethods[method] = struct{}{}
		}
	}
}

// UnaryServerLog server-side log unary interceptor
func UnaryServerLog(opts ...LogOption) grpc.UnaryServerInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)

	if o.isReplaceGRPCLogger {
		logger.ReplaceGRPCLoggerV2(logger.Get())
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ignore printing of the specified method
		if _, ok := o.ignoreMethods[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		startTime := time.Now()
		requestID := ServerCtxRequestID(ctx)

		fields := []logger.Field{
			logger.String("type", "unary"),
			logger.String("method", info.FullMethod),
			logger.Any("request", req),
		}
		if requestID != "" {
			fields = append(fields, logger.String(string(logger.ContextKeyRequestID), requestID))
		}
		fields = append(fields, logger.String("log_from", o.logFrom+" <<<<"))
		logger.InfoWithCtx(ctx, `grpc interceptor UnaryServerLog`, fields...)

		resp, err := handler(ctx, req)

		data, marshalErr := json.Marshal(resp)
		if marshalErr != nil {
			logger.WarnWithCtx(ctx, "marshal response error", logger.Err(marshalErr))
			data = []byte("{}")
		}
		if len(data) > o.maxLength {
			data = append(data[:o.maxLength], []byte("......")...)
		}
		fields = []logger.Field{
			logger.String("code", status.Code(err).String()),
			logger.String("type", "unary"),
			logger.String("method", info.FullMethod),
			logger.String("response", string(data)),
			logger.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if err != nil {
			fields = append(fields, logger.Err(err))
		}
		if requestID != "" {
			fields = append(fields, logger.String(string(logger.ContextKeyRequestID), requestID))
		}
		fields = append(fields, logger.String("log_from", o.logFrom+" >>>>"))
		logger.InfoWithCtx(ctx, `grpc interceptor UnaryServerLog`, fields...)

		return resp, err
	}
}

// UnaryServerSimpleLog server-side log unary interceptor, only print response
func UnaryServerSimpleLog(opts ...LogOption) grpc.UnaryServerInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)

	if o.isReplaceGRPCLogger {
		logger.ReplaceGRPCLoggerV2(logger.Get())
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ignore printing of the specified method
		if _, ok := o.ignoreMethods[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		startTime := time.Now()
		requestID := ServerCtxRequestID(ctx)

		resp, err := handler(ctx, req)

		fields := []logger.Field{
			logger.String("code", status.Code(err).String()),
			logger.String("type", "unary"),
			logger.String("method", info.FullMethod),
			logger.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if err != nil {
			fields = append(fields, logger.Err(err))
		}
		if requestID != "" {
			fields = append(fields, logger.String(string(logger.ContextKeyRequestID), requestID))
		}
		fields = append(fields, logger.String("log_from", o.logFrom+` [GRPC] UnaryServerSimpleLog`))
		logger.InfoWithCtx(ctx, `[GRPC]`, fields...)

		return resp, err
	}
}

// StreamServerLog Server-side log stream interceptor
func StreamServerLog(opts ...LogOption) grpc.StreamServerInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)

	if o.isReplaceGRPCLogger {
		logger.ReplaceGRPCLoggerV2(logger.Get())
	}

	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// ignore printing of the specified method
		if _, ok := o.ignoreMethods[info.FullMethod]; ok {
			return handler(srv, stream)
		}

		startTime := time.Now()
		requestID := ServerCtxRequestID(stream.Context())

		fields := []logger.Field{
			logger.String("type", "stream"),
			logger.String("method", info.FullMethod),
		}
		if requestID != "" {
			fields = append(fields, logger.String(string(logger.ContextKeyRequestID), requestID))
		}
		fields = append(fields, logger.String("log_from", " <<<<"))

		logger.InfoWithCtx(stream.Context(), `grpc interceptor StreamServerLog`, fields...)

		err := handler(srv, stream)

		fields = []logger.Field{
			logger.String("code", status.Code(err).String()),
			logger.String("type", "stream"),
			logger.String("method", info.FullMethod),
			logger.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if requestID != "" {
			fields = append(fields, logger.String(string(logger.ContextKeyRequestID), requestID))
		}
		fields = append(fields, logger.String("log_from", o.logFrom+` >>>>`))
		logger.InfoWithCtx(stream.Context(), `grpc interceptor StreamServerLog`, fields...)

		return err
	}
}

// StreamServerSimpleLog Server-side log stream interceptor, only print response
func StreamServerSimpleLog(opts ...LogOption) grpc.StreamServerInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)

	if o.isReplaceGRPCLogger {
		logger.ReplaceGRPCLoggerV2(logger.Get())
	}

	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// ignore printing of the specified method
		if _, ok := o.ignoreMethods[info.FullMethod]; ok {
			return handler(srv, stream)
		}

		startTime := time.Now()
		requestID := ServerCtxRequestID(stream.Context())

		err := handler(srv, stream)

		fields := []logger.Field{
			logger.String("code", status.Code(err).String()),
			logger.String("type", "stream"),
			logger.String("method", info.FullMethod),
			logger.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if requestID != "" {
			fields = append(fields, logger.String(string(logger.ContextKeyRequestID), requestID))
		}

		fields = append(fields, logger.String("log_from", o.logFrom+` [GRPC] StreamServerSimpleLog`))
		logger.InfoWithCtx(stream.Context(), `[GRPC]`, fields...)
		return err
	}
}
