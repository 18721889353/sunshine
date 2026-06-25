// Package gows 提供 WebSocket 服务端封装，包含连接管理、消息读写、心跳保活和全局分发。
package gows

import (
	"context"
	"errors"

	"github.com/spf13/cast"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/jwt"
)

// ErrTokenInvalid token 无效（格式正确但缺少 uid 字段）
var ErrTokenInvalid = errors.New("token is invalid: missing uid")

// defaultUIDFields UID 字段名优先级列表，与 middleware 保持一致
var defaultUIDFields = []string{"id", "uid", "userId", "user_id"}

// extractUID 从 claims 中提取用户标识，优先使用 UID 字段，
// 其次按优先级遍历 claims.Fields 中的备选字段名。
func extractUID(claims *jwt.Claims) string {
	if claims.UID != "" {
		return claims.UID
	}

	for _, key := range defaultUIDFields {
		if val, ok := claims.Fields[key]; ok {
			if str, ok := val.(string); ok && str != "" {
				return str
			}
			if str := cast.ToString(val); str != "" {
				return str
			}
		}
	}

	return ""
}

// ParseTokenCtx 解析并验证 JWT token，返回用户标识（带 context 的版本）。
//
// 参数:
//   - ctx: 上下文，用于链路追踪传播（其中应包含 request_id）
//   - tokenString: JWT token 字符串，支持带 "Bearer " 前缀或不带前缀两种格式
//
// 返回:
//   - string: 用户标识（UID），token 有效时返回
//   - error: 解析失败时返回具体错误信息
//
// 注意:
//   - 使用前需确保 jwt.Init() 已被调用，通常在应用启动时初始化
//   - token 过期返回 jwt.ErrTokenExpired，调用方可用 errors.Is 判断
//   - token 格式正确但缺少 uid 字段返回 ErrTokenInvalid
//   - 自动去除 "Bearer " 前缀，兼容 Authorization header 传入的 token
func ParseTokenCtx(ctx context.Context, tokenString string) (string, error) {
	tracer := otel.Tracer("gows")
	_, span := tracer.Start(ctx, "ws.parse_token", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	span.SetAttributes(requestIDAttr(ctx))

	if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
		tokenString = tokenString[7:]
	}

	claims, err := jwt.ParseToken(tokenString)
	if err != nil {
		span.SetAttributes(attribute.String("ws.token_error", err.Error()))
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}

	uid := extractUID(claims)
	if uid != "" {
		span.SetAttributes(attribute.String("ws.uid", uid))
		span.SetStatus(codes.Ok, "token parsed")
		return uid, nil
	}

	span.SetStatus(codes.Error, "token missing uid")
	return "", ErrTokenInvalid
}
