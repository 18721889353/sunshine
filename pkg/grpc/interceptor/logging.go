package interceptor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	pkgLogger "github.com/18721889353/sunshine/pkg/logger"
)

// ---------------------------------- client interceptor ----------------------------------

// UnaryClientLog client log unary interceptor
func UnaryClientLog(logger *zap.Logger, opts ...LogOption) grpc.UnaryClientInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	if o.isReplaceGRPCLogger {
		pkgLogger.ReplaceGRPCLoggerV2(logger)
	}

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		startTime := time.Now()

		var reqIDField zap.Field
		if requestID := ClientCtxRequestID(ctx); requestID != "" {
			reqIDField = zap.String(ContextRequestIDKey, requestID)
		} else {
			reqIDField = zap.Skip()
		}
		fields := []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("type", "unary"),
			zap.String("method", method),
			pkgLogger.Any("request", req),
			reqIDField,
		}
		fields = append(fields, zap.String("log_from", o.logFrom+" invoker request UnaryClientLog"))
		pkgLogger.Info("invoker request", fields...)

		err := invoker(ctx, method, req, reply, cc, opts...)

		fields = []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("code", status.Code(err).String()),
			zap.String("type", "unary"),
			zap.String("method", method),
			pkgLogger.Any("reply", reply),

			zap.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
			reqIDField,
		}
		if err != nil {
			fields = append(fields, zap.String("err", err.Error()))
		}

		fields = append(fields, zap.String("log_from", o.logFrom+" invoker result UnaryClientLog"))
		pkgLogger.Info("invoker result", fields...)
		return err
	}
}

// StreamClientLog client log stream interceptor
func StreamClientLog(logger *zap.Logger, opts ...LogOption) grpc.StreamClientInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	if o.isReplaceGRPCLogger {
		pkgLogger.ReplaceGRPCLoggerV2(logger)
	}

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
		streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		startTime := time.Now()

		var reqIDField zap.Field
		if requestID := ClientCtxRequestID(ctx); requestID != "" {
			reqIDField = zap.String(ContextRequestIDKey, requestID)
		} else {
			reqIDField = zap.Skip()
		}

		clientStream, err := streamer(ctx, desc, cc, method, opts...)

		fields := []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("code", status.Code(err).String()),
			zap.String("type", "stream"),
			zap.String("method", method),

			zap.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),

			reqIDField,
		}
		if err != nil {
			fields = append(fields, zap.String("err", err.Error()))
		}

		fields = append(fields, zap.String("log_from", "gw StreamClientLog"))
		logger.Info("invoker result", fields...)

		return clientStream, err
	}
}

// ---------------------------------- server interceptor ----------------------------------

var ignoreLogMethods = map[string]struct{}{} // ignore printing methods

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
func UnaryServerLog(logger *zap.Logger, opts ...LogOption) grpc.UnaryServerInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)
	ignoreLogMethods = o.ignoreMethods

	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	if o.isReplaceGRPCLogger {
		pkgLogger.ReplaceGRPCLoggerV2(logger)
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ignore printing of the specified method
		if _, ok := ignoreLogMethods[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		startTime := time.Now()
		requestID := ServerCtxRequestID(ctx)

		fields := []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("type", "unary"),
			zap.String("method", info.FullMethod),
			zap.Any("request", req),
		}
		if requestID != "" {
			fields = append(fields, zap.String(ContextRequestIDKey, requestID))
		}
		fields = append(fields, zap.String("log_from", o.logFrom+" request UnaryServerLog"))
		pkgLogger.Info(`<<<<`, fields...)

		resp, err := handler(ctx, req)

		data, _ := json.Marshal(resp)
		if len(data) > o.maxLength {
			data = append(data[:o.maxLength], []byte("......")...)
		}
		fields = []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("code", status.Code(err).String()),
			zap.String("type", "unary"),
			zap.String("method", info.FullMethod),
			zap.String("response", string(data)),
			zap.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if err != nil {
			fields = append(fields, zap.String("err", err.Error()))
		}
		if requestID != "" {
			fields = append(fields, zap.String(ContextRequestIDKey, requestID))
		}
		fields = append(fields, zap.String("log_from", o.logFrom+" response UnaryServerLog"))
		pkgLogger.Info(`>>>>`, fields...)

		return resp, err
	}
}

// UnaryServerSimpleLog server-side log unary interceptor, only print response
func UnaryServerSimpleLog(logger *zap.Logger, opts ...LogOption) grpc.UnaryServerInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)
	ignoreLogMethods = o.ignoreMethods

	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	if o.isReplaceGRPCLogger {
		pkgLogger.ReplaceGRPCLoggerV2(logger)
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ignore printing of the specified method
		if _, ok := ignoreLogMethods[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		startTime := time.Now()
		requestID := ServerCtxRequestID(ctx)

		resp, err := handler(ctx, req)

		fields := []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("code", status.Code(err).String()),
			zap.String("type", "unary"),
			zap.String("method", info.FullMethod),

			zap.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if err != nil {
			fields = append(fields, zap.String("err", err.Error()))
		}
		if requestID != "" {
			fields = append(fields, zap.String(ContextRequestIDKey, requestID))
		}
		fields = append(fields, zap.String("log_from", o.logFrom+` [GRPC] UnaryServerSimpleLog`))
		pkgLogger.Info(`[GRPC]`, fields...)

		return resp, err
	}
}

// StreamServerLog Server-side log stream interceptor
func StreamServerLog(logger *zap.Logger, opts ...LogOption) grpc.StreamServerInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)
	ignoreLogMethods = o.ignoreMethods

	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	if o.isReplaceGRPCLogger {
		pkgLogger.ReplaceGRPCLoggerV2(logger)
	}

	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// ignore printing of the specified method
		if _, ok := ignoreLogMethods[info.FullMethod]; ok {
			return handler(srv, stream)
		}

		startTime := time.Now()
		requestID := ServerCtxRequestID(stream.Context())

		fields := []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("type", "stream"),
			zap.String("method", info.FullMethod),
		}
		if requestID != "" {
			fields = append(fields, zap.String(ContextRequestIDKey, requestID))
		}
		fields = append(fields, zap.String("log_from", "gw StreamServerLog"))

		pkgLogger.Info(`<<<<`, fields...)

		err := handler(srv, stream)

		fields = []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("code", status.Code(err).String()),
			zap.String("type", "stream"),
			zap.String("method", info.FullMethod),

			zap.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if requestID != "" {
			fields = append(fields, zap.String(ContextRequestIDKey, requestID))
		}
		fields = append(fields, zap.String("log_from", o.logFrom+` >>>> StreamServerLog`))
		pkgLogger.Info(`>>>>`, fields...)

		return err
	}
}

// StreamServerSimpleLog Server-side log stream interceptor, only print response
func StreamServerSimpleLog(logger *zap.Logger, opts ...LogOption) grpc.StreamServerInterceptor {
	o := defaultLogOptions()
	o.apply(opts...)
	ignoreLogMethods = o.ignoreMethods

	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	if o.isReplaceGRPCLogger {
		pkgLogger.ReplaceGRPCLoggerV2(logger)
	}

	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// ignore printing of the specified method
		if _, ok := ignoreLogMethods[info.FullMethod]; ok {
			return handler(srv, stream)
		}

		startTime := time.Now()
		requestID := ServerCtxRequestID(stream.Context())

		err := handler(srv, stream)

		fields := []zap.Field{
			zap.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
			zap.String("code", status.Code(err).String()),
			zap.String("type", "stream"),
			zap.String("method", info.FullMethod),

			zap.String("ms", fmt.Sprintf("%v", float64(time.Since(startTime).Nanoseconds())/1e6)),
		}
		if requestID != "" {
			fields = append(fields, zap.String(ContextRequestIDKey, requestID))
		}

		fields = append(fields, zap.String("log_from", o.logFrom+` [GRPC] StreamServerSimpleLog`))
		pkgLogger.Info(`[GRPC]`, fields...)
		return err
	}
}
