package interceptor

import (
	"context"
	"sync/atomic"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"google.golang.org/grpc"
)

// tokenEnabled 控制服务端 Token 认证是否生效，支持热更新
var tokenEnabled atomic.Bool

// SetTokenEnabled 动态设置服务端 Token 认证是否生效，支持 Nacos 热更新。
// enabled=true 时启用 Token 校验，enabled=false 时所有请求直接放行。
func SetTokenEnabled(enabled bool) {
	tokenEnabled.Store(enabled)
}

// ---------------------------------- client option ----------------------------------

type authToken struct {
	AppID    string `json:"app_id"`
	AppKey   string `json:"app_key"`
	IsSecure bool   `json:"isSecure"`
}

// GetRequestMetadata get metadata
func (t *authToken) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) { //nolint
	return map[string]string{
		"app_id":  t.AppID,
		"app_key": t.AppKey,
	}, nil
}

// RequireTransportSecurity is require transport secure
func (t *authToken) RequireTransportSecurity() bool {
	return t.IsSecure
}

// ClientTokenOption client token
func ClientTokenOption(appID string, appKey string, isSecure bool) grpc.DialOption {
	return grpc.WithPerRPCCredentials(&authToken{appID, appKey, isSecure})
}

// ---------------------------------- server interceptor ----------------------------------

// CheckToken check app id and app key
// Example:
//
//	var f CheckToken=func(appID string, appKey string) error{
//		if appID != targetAppID || appKey != targetAppKey {
//			return status.Errorf(codes.Unauthenticated, "app id or app key checksum failure")
//		}
//		return nil
//	}
type CheckToken func(appID string, appKey string) error

// UnaryServerToken recovery unary token
func UnaryServerToken(f CheckToken) grpc.UnaryServerInterceptor {
	// 初始化开关为启用状态（调用方可随后通过 SetTokenEnabled 覆盖）
	tokenEnabled.Store(true)
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 热更新开关检查
		if !tokenEnabled.Load() {
			return handler(ctx, req)
		}
		appID := metautils.ExtractIncoming(ctx).Get("app_id")
		appKey := metautils.ExtractIncoming(ctx).Get("app_key")
		err := f(appID, appKey)
		if err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

// StreamServerToken recovery stream token
func StreamServerToken(f CheckToken) grpc.StreamServerInterceptor {
	// 初始化开关为启用状态（调用方可随后通过 SetTokenEnabled 覆盖）
	tokenEnabled.Store(true)
	return func(srv interface{}, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// 热更新开关检查
		if !tokenEnabled.Load() {
			return handler(srv, stream)
		}
		ctx := stream.Context()
		appID := metautils.ExtractIncoming(ctx).Get("app_id")
		appKey := metautils.ExtractIncoming(ctx).Get("app_key")
		err := f(appID, appKey)
		if err != nil {
			return err
		}

		return handler(srv, stream)
	}
}
