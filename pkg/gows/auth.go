package gows

import (
	"errors"

	"github.com/spf13/cast"

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

// ParseToken 解析并验证 JWT token，返回用户标识。
//
// 参数:
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
func ParseToken(tokenString string) (string, error) {
	// 兼容 Authorization header 传入的 "Bearer " 前缀格式
	if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
		tokenString = tokenString[7:]
	}

	claims, err := jwt.ParseToken(tokenString)
	if err != nil {
		return "", err
	}

	uid := extractUID(claims)
	if uid != "" {
		return uid, nil
	}

	return "", ErrTokenInvalid
}
