// Package middleware 提供 Gin 框架的 JWT 认证中间件。
// 支持 Bearer Token 解析、自定义验证函数、用户 ID 提取、URL 忽略列表及全局忽略开关。
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"

	"github.com/18721889353/sunshine/pkg/errcode"
	"github.com/18721889353/sunshine/pkg/gin/response"
	"github.com/18721889353/sunshine/pkg/jwt"
	"github.com/18721889353/sunshine/pkg/logger"
)

const (
	// HeaderAuthorizationKey 定义 HTTP 请求头中承载 JWT Token 的字段名，标准为 "Authorization"
	HeaderAuthorizationKey = "Authorization"
)

// jwtOptions 保存 JWT 中间件的所有配置项
type jwtOptions struct {
	isSwitchHTTPCode bool                // 是否使用 HTTP 状态码响应（默认使用业务码）
	verify           VerifyFn            // 自定义验证函数（仅在 Auth 中使用）
	ignoreMethods    map[string]struct{} // 需要忽略 JWT 验证的 URL 列表（精确匹配）
	uidFields        []string            // 用户 ID 字段名优先级列表，用于从 Claims.Fields 中提取 UID
	ignoreAll        bool                // 是否全局忽略所有请求的 JWT 验证（优先级最高）
}

// JwtOption 定义 JWT 中间件配置的函数选项类型
type JwtOption func(*jwtOptions)

// apply 依次应用传入的选项
func (o *jwtOptions) apply(opts ...JwtOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// defaultJwtOptions 返回默认配置
func defaultJwtOptions() *jwtOptions {
	return &jwtOptions{
		isSwitchHTTPCode: false,
		verify:           nil,
		ignoreMethods:    make(map[string]struct{}),
		uidFields:        []string{"id", "uid", "userId", "user_id"}, // 按优先级排列
	}
}

// WithSwitchHTTPCode 设置使用标准 HTTP 状态码（如 401）返回未授权错误，
// 默认使用业务错误码（errcode.Unauthorized）通过 response.Error 返回。
func WithSwitchHTTPCode() JwtOption {
	return func(o *jwtOptions) {
		o.isSwitchHTTPCode = true
	}
}

// WithVerify 设置自定义验证函数，该函数会在 Token 解析成功后调用，
// 可用于额外的权限校验或状态检查。若未设置，则仅提取用户信息并设置到 gin.Context 中。
func WithVerify(verify VerifyFn) JwtOption {
	return func(o *jwtOptions) {
		o.verify = verify
	}
}

// WithJwtIgnoreMethods 设置需要忽略 JWT 验证的 URL 列表（精确匹配路径）。
// 注意：虽然参数名为 fullMethodNames，但实际对应的是 HTTP 请求的 URL.Path。
// 示例："/api/v1/health", "/api/v1/metrics"
func WithJwtIgnoreMethods(fullMethodNames ...string) JwtOption {
	return func(o *jwtOptions) {
		for _, method := range fullMethodNames {
			o.ignoreMethods[method] = struct{}{}
		}
	}
}

// WithAuthUIDFields 设置用户 ID 字段的优先级列表。
// 当 Claims.UID 为空时，会按顺序从 Claims.Fields 中查找指定的字段名，
// 并将第一个非空值作为用户 ID 设置到 gin.Context 的 "uid" 键中。
func WithAuthUIDFields(fields ...string) JwtOption {
	return func(o *jwtOptions) {
		if len(fields) > 0 {
			o.uidFields = fields
		}
	}
}

// WithAuthIgnoreAll 设置忽略所有请求的 JWT 验证。
// 启用后，中间件将直接放行所有请求，不解析 Token 也不进行任何权限校验。
// 此选项通常用于开发、测试环境或内部服务调用，生产环境请勿启用。
func WithAuthIgnoreAll() JwtOption {
	return func(o *jwtOptions) {
		o.ignoreAll = true
	}
}

// responseUnauthorized 统一处理未授权响应
// 根据 isSwitchHTTPCode 决定使用 HTTP 状态码还是业务错误码返回
func responseUnauthorized(c *gin.Context, isSwitchHTTPCode bool) {
	if isSwitchHTTPCode {
		response.Out(c, errcode.Unauthorized)
	} else {
		response.Error(c, errcode.Unauthorized)
	}
}

// -------------------------------------------------------------------------------------------

// VerifyFn 自定义验证函数类型。
// 参数 claims 为解析后的 JWT 标准 Claims，tokenTail10 为 Token 的最后 10 个字符（可用于缓存或日志），
// c 为当前的 gin.Context，便于获取请求信息或设置上下文。
// 若返回非 nil 错误，则认证失败，中间件会返回 401/业务未授权。
type VerifyFn func(claims *jwt.Claims, tokenTail10 string, c *gin.Context) error

// extractUIDFromClaims 从 Claims 中按优先级提取用户 ID。
// 优先使用 claims.UID，若为空则遍历 uidFields 从 claims.Fields 中查找第一个非空字符串。
func extractUIDFromClaims(claims *jwt.Claims, uidFields []string) string {
	uid := claims.UID
	if uid == "" {
		for _, key := range uidFields {
			if val, ok := claims.Fields[key]; ok {
				if str, ok := val.(string); ok && str != "" {
					uid = str
					break
				}
				// 若不是字符串类型，使用 cast 进行类型转换
				if str := cast.ToString(val); str != "" {
					uid = str
					break
				}
			}
		}
	}
	return uid
}

// handleAuthVerification 处理认证验证流程
// 若配置了 VerifyFn，则调用该函数；否则直接从 Claims 中提取 UID 和 Name 并存入 gin.Context。
// 返回错误时，调用方应中止后续处理。
func handleAuthVerification(o *jwtOptions, claims *jwt.Claims, token string, c *gin.Context) error {
	if o.verify != nil {
		tokenTail10 := token[len(token)-10:]
		if err := o.verify(claims, tokenTail10, c); err != nil {
			logger.WarnWithCtx(c.Request.Context(), "verify error",
				logger.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
				logger.String("method", c.Request.Method),
				logger.String("url", c.Request.URL.String()),
				logger.Err(err),
				logger.Any("claims", claims),
				logger.String("uid", claims.UID),
				logger.String("name", claims.Name))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return err
		}
	} else {
		// 提取用户信息并设置到 Context 中，供后续业务使用
		uid := extractUIDFromClaims(claims, o.uidFields)
		c.Set("uid", uid)
		c.Set("name", claims.Name)
	}
	return nil
}

// Auth 创建 Gin JWT 认证中间件。
// 验证流程：
//  1. 若开启 ignoreAll，直接放行
//  2. 若当前请求 URL 在 ignoreMethods 中，直接放行
//  3. 否则从 Authorization 头提取 Bearer Token，解析 JWT
//  4. 执行自定义验证（若有），或提取用户信息存入 Context
//  5. 验证通过则继续，否则返回未授权错误
func Auth(opts ...JwtOption) gin.HandlerFunc {
	o := defaultJwtOptions()
	o.apply(opts...)
	return func(c *gin.Context) {
		// 优先判断是否全局忽略
		if o.ignoreAll {
			c.Next()
			return
		}

		// 其次判断是否在忽略 URL 列表中
		if _, ok := o.ignoreMethods[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		authorization := c.GetHeader(HeaderAuthorizationKey)
		// 检查 Authorization 头是否存在且长度足够（至少 "Bearer " + token，token 通常较长）
		if len(authorization) < 150 {
			logger.WarnWithCtx(c.Request.Context(), "authorization is illegal",
				logger.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
				logger.String("method", c.Request.Method),
				logger.String("url", c.Request.URL.String()),
				logger.String(HeaderAuthorizationKey, authorization))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		// 去除 "Bearer " 前缀（长度为 7）
		token := authorization[7:]
		claims, err := jwt.ParseToken(token)
		if err != nil {
			logger.WarnWithCtx(c.Request.Context(), "ParseToken error",
				logger.String("current_time", time.Now().Format("2006-01-02 15:04:05.000000000")),
				logger.String("method", c.Request.Method),
				logger.String("url", c.Request.URL.String()),
				logger.String("token", token),
				logger.Err(err))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		// 处理验证（自定义验证或提取用户信息）
		if err := handleAuthVerification(o, claims, token, c); err != nil {
			return
		}

		c.Next()
	}
}

// -------------------------------------------------------------------------------------------

// VerifyCustomFn 自定义验证函数类型（用于 AuthCustom）。
// 与 VerifyFn 类似，但 claims 类型为 jwt.CustomClaims，支持更丰富的自定义字段。
type VerifyCustomFn func(claims *jwt.CustomClaims, tokenTail10 string, c *gin.Context) error

// AuthCustom 创建使用自定义 Claims 的 JWT 认证中间件。
// 该中间件同样支持 WithIgnoreAll() 选项，启用后全局跳过所有鉴权。
// 验证流程与 Auth 一致，但使用 jwt.ParseCustomToken 解析 Token，并调用传入的 verify 函数。
func AuthCustom(verify VerifyCustomFn, opts ...JwtOption) gin.HandlerFunc {
	o := defaultJwtOptions()
	o.apply(opts...)

	return func(c *gin.Context) {
		// 优先判断是否全局忽略
		if o.ignoreAll {
			c.Next()
			return
		}

		authorization := c.GetHeader(HeaderAuthorizationKey)
		if len(authorization) < 150 {
			logger.WarnWithCtx(c.Request.Context(), "authorization is illegal")
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		token := authorization[7:]
		claims, err := jwt.ParseCustomToken(token)
		if err != nil {
			logger.WarnWithCtx(c.Request.Context(), "ParseToken error", logger.Err(err))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		tokenTail10 := token[len(token)-10:]
		if err = verify(claims, tokenTail10, c); err != nil {
			logger.WarnWithCtx(c.Request.Context(), "verify error", logger.Err(err), logger.Any("fields", claims.Fields))
			responseUnauthorized(c, o.isSwitchHTTPCode)
			c.Abort()
			return
		}

		c.Next()
	}
}
