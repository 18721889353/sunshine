// Package interceptor 提供 gRPC 客户端和服务端常用的拦截器，
// 包括 JWT 认证、日志、限流、熔断和指标采集等。
// 本文件实现基于 JWT 的认证拦截器，支持标准 Claims 和自定义 Claims。
package interceptor

import (
	"context"
	"errors"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/18721889353/sunshine/pkg/jwt"
	"github.com/18721889353/sunshine/pkg/utils"
)

// ---------------------------------- client ----------------------------------

// SetJwtTokenToCtx 将 JWT Token（不含 "Bearer " 前缀）设置到 gRPC 客户端上下文中，
// 以便在后续的 RPC 调用中自动附加到请求头。
// 参数 token 为纯 JWT 字符串，示例：SetJwtTokenToCtx(ctx, "eyJhbGci...")
func SetJwtTokenToCtx(ctx context.Context, token string) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		md.Set(headerAuthorize, authScheme+" "+token)
	} else {
		md = metadata.Pairs(headerAuthorize, authScheme+" "+token)
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// SetAuthToCtx 将完整的 Authorization 头值（包含 Scheme，如 "Bearer <token>"）
// 设置到 gRPC 客户端上下文中。
// 参数 authorization 为完整的头值，示例：SetAuthToCtx(ctx, "Bearer eyJhbGci...")
func SetAuthToCtx(ctx context.Context, authorization string) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		md.Set(headerAuthorize, authorization)
	} else {
		md = metadata.Pairs(headerAuthorize, authorization)
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// ---------------------------------- server interceptor ----------------------------------

var (
	// headerAuthorize 定义 gRPC metadata 中承载认证信息的键名
	headerAuthorize = "authorization"

	// authScheme 默认的认证方案（Bearer Token）
	authScheme = "Bearer"

	// authCtxClaimsName 定义在 context 中存储 Claims 的键名
	authCtxClaimsName = "tokenInfo"

	// authIgnoreMethods 存储需要忽略认证的方法全名（精确匹配），
	// 由 WithAuthIgnoreMethods 选项填充，包级变量用于兼容原有设计，
	// 但推荐使用 authOptions.ignoreMethods 和 ignoreAll 进行配置。
	authIgnoreMethods = map[string]struct{}{}
)

// GetAuthorization 将纯 Token 组合成标准 Authorization 头值（含 Scheme）
func GetAuthorization(token string) string {
	return authScheme + " " + token
}

// GetAuthCtxKey 返回在 context 中存储 Claims 的键名
func GetAuthCtxKey() string {
	return authCtxClaimsName
}

// StandardVerifyFn 定义标准 Claims 的自定义验证函数类型。
// 参数 claims 为解析后的 jwt.Claims，tokenTail32 为 Token 后 32 个字符（可用于缓存或日志），
// ctx 为当前上下文，便于获取请求信息。
// 若返回 error 则认证失败。
type StandardVerifyFn = func(claims *jwt.Claims, tokenTail32 string, ctx context.Context) error

// CustomVerifyFn 定义自定义 Claims 的验证函数类型。
type CustomVerifyFn = func(claims *jwt.CustomClaims, tokenTail32 string, ctx context.Context) error

// verifyOptions 保存验证函数的配置（区分标准或自定义）
type verifyOptions struct {
	verifyType       int // 1: StandardVerifyFn, 2: CustomVerifyFn
	standardVerifyFn StandardVerifyFn
	customVerifyFn   CustomVerifyFn
}

// defaultVerifyOptions 返回默认验证配置（使用标准 Claims）
func defaultVerifyOptions() *verifyOptions {
	return &verifyOptions{
		verifyType: 1,
	}
}

// AuthOption 定义认证配置的函数选项类型
type AuthOption func(*authOptions)

// authOptions 保存认证拦截器的所有配置项
type authOptions struct {
	authScheme    string              // 认证方案（如 "Bearer"）
	ctxClaimsName string              // 在 context 中存储 Claims 的键名
	ignoreMethods map[string]struct{} // 需要忽略认证的方法全名列表
	ignoreAll     bool                // 是否全局忽略所有方法的认证（优先级最高）
	verifyOpts    *verifyOptions      // 验证函数配置
}

// defaultAuthOptions 返回默认配置
func defaultAuthOptions() *authOptions {
	return &authOptions{
		authScheme:    authScheme,
		ctxClaimsName: authCtxClaimsName,
		ignoreMethods: make(map[string]struct{}),
		verifyOpts:    defaultVerifyOptions(),
	}
}

// apply 依次应用传入的选项
func (o *authOptions) apply(opts ...AuthOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithAuthScheme 设置认证方案（默认 "Bearer"）
func WithAuthScheme(scheme string) AuthOption {
	return func(o *authOptions) {
		o.authScheme = scheme
	}
}

// WithAuthClaimsName 设置在 context 中存储 Claims 的键名（默认 "tokenInfo"）
func WithAuthClaimsName(claimsName string) AuthOption {
	return func(o *authOptions) {
		o.ctxClaimsName = claimsName
	}
}

// WithAuthIgnoreMethods 设置需要忽略认证的方法列表（精确匹配方法全名）。
// fullMethodName 格式：/packageName.serviceName/methodName
// 示例：/api.userExample.v1.userExampleService/GetByID
func WithAuthIgnoreMethods(fullMethodNames ...string) AuthOption {
	return func(o *authOptions) {
		for _, method := range fullMethodNames {
			o.ignoreMethods[method] = struct{}{}
		}
	}
}

// WithAuthIgnoreAll 设置忽略所有方法的认证。
// 启用后，所有请求均跳过 JWT 验证，直接放行。
// 此选项优先级高于 WithAuthIgnoreMethods 和正常验证流程，
// 通常用于开发、测试环境或内部服务调用，生产环境请勿启用。
func WithAuthIgnoreAll() AuthOption {
	return func(o *authOptions) {
		o.ignoreAll = true
	}
}

// WithStandardVerify 设置标准 Claims 的自定义验证函数
func WithStandardVerify(verify StandardVerifyFn) AuthOption {
	return func(o *authOptions) {
		if o.verifyOpts == nil {
			o.verifyOpts = defaultVerifyOptions()
		}
		o.verifyOpts.verifyType = 1
		o.verifyOpts.standardVerifyFn = verify
	}
}

// WithCustomVerify 设置自定义 Claims 的验证函数
func WithCustomVerify(verify CustomVerifyFn) AuthOption {
	return func(o *authOptions) {
		if o.verifyOpts == nil {
			o.verifyOpts = defaultVerifyOptions()
		}
		o.verifyOpts.verifyType = 2
		o.verifyOpts.customVerifyFn = verify
	}
}

// -------------------------------------------------------------------------------------------

// jwtVerify 从上下文中提取并验证 JWT Token，支持标准和自定义 Claims。
// 返回包含 Claims 的新 context（通过 WithValue 存储）。
func jwtVerify(ctx context.Context, opt *verifyOptions) (context.Context, error) {
	if opt == nil {
		opt = &verifyOptions{
			verifyType: 1,
		}
	}

	// 从 metadata 中提取 Token（自动去除 Scheme 前缀）
	token, err := grpc_auth.AuthFromMD(ctx, authScheme)
	if err != nil {
		return ctx, status.Errorf(codes.Unauthenticated, "%v", err)
	}

	// 简单校验 Token 长度（防止明显非法）
	if len(token) <= 100 {
		return ctx, status.Errorf(codes.Unauthenticated, "authorization is illegal")
	}

	// 处理自定义 Claims
	if opt.verifyType == 2 {
		var claims *jwt.CustomClaims
		claims, err = jwt.ParseCustomToken(token)
		if err != nil {
			return ctx, status.Errorf(codes.Unauthenticated, "%v", err)
		}
		if opt.customVerifyFn != nil {
			tokenTail32 := token[len(token)-16:] // 取后 16 位用于缓存/日志
			err = opt.customVerifyFn(claims, tokenTail32, ctx)
			if err != nil {
				return ctx, status.Errorf(codes.Unauthenticated, "%v", err)
			}
		}
		newCtx := context.WithValue(ctx, authCtxClaimsName, claims) //nolint
		return newCtx, nil
	}

	// 处理标准 Claims
	claims, err := jwt.ParseToken(token)
	if err != nil {
		return ctx, status.Errorf(codes.Unauthenticated, "%v", err)
	}
	if opt.standardVerifyFn != nil {
		tokenTail32 := token[len(token)-16:]
		err = opt.standardVerifyFn(claims, tokenTail32, ctx)
		if err != nil {
			return ctx, status.Errorf(codes.Unauthenticated, "%v", err)
		}
	}
	newCtx := context.WithValue(ctx, authCtxClaimsName, claims) //nolint
	return newCtx, nil
}

// GetJwtClaims 从 context 中获取标准 jwt.Claims（若存储的是标准 Claims）
func GetJwtClaims(ctx context.Context) (*jwt.Claims, bool) {
	v, ok := ctx.Value(authCtxClaimsName).(*jwt.Claims)
	return v, ok
}

// GetJwtCustomClaims 从 context 中获取自定义 jwt.CustomClaims（若存储的是自定义 Claims）
func GetJwtCustomClaims(ctx context.Context) (*jwt.CustomClaims, bool) {
	v, ok := ctx.Value(authCtxClaimsName).(*jwt.CustomClaims)
	return v, ok
}

// UnaryServerJwtAuth 创建一元服务端 JWT 认证拦截器。
// 验证流程：
//  1. 若开启 ignoreAll，直接放行
//  2. 若当前方法在 ignoreMethods 中，直接放行
//  3. 否则执行 JWT 解析和自定义验证（若有）
func UnaryServerJwtAuth(opts ...AuthOption) grpc.UnaryServerInterceptor {
	o := defaultAuthOptions()
	o.apply(opts...)
	// 更新全局变量（兼容已有代码）
	authScheme = o.authScheme
	authCtxClaimsName = o.ctxClaimsName
	authIgnoreMethods = o.ignoreMethods
	verifyOpt := o.verifyOpts

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 优先判断全局忽略
		if o.ignoreAll {
			return handler(ctx, req)
		}

		// 其次判断是否在忽略方法列表中
		if _, ok := authIgnoreMethods[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		// 添加完整方法名到 metadata（便于日志追踪）
		ctx = metautils.ExtractIncoming(ctx).Add("grpc-full-method", info.FullMethod).ToIncoming(ctx)

		// 执行 JWT 验证
		newCtx, err := jwtVerify(ctx, verifyOpt)
		if err != nil {
			return nil, err
		}
		return handler(newCtx, req)
	}
}

// StreamServerJwtAuth 创建流式服务端 JWT 认证拦截器。
// 验证流程同 UnaryServerJwtAuth，适用于服务端流式 RPC。
func StreamServerJwtAuth(opts ...AuthOption) grpc.StreamServerInterceptor {
	o := defaultAuthOptions()
	o.apply(opts...)
	authScheme = o.authScheme
	authCtxClaimsName = o.ctxClaimsName
	authIgnoreMethods = o.ignoreMethods
	verifyOpt := o.verifyOpts

	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// 优先判断全局忽略
		if o.ignoreAll {
			return handler(srv, stream)
		}

		if _, ok := authIgnoreMethods[info.FullMethod]; ok {
			return handler(srv, stream)
		}

		newCtx, err := jwtVerify(stream.Context(), verifyOpt)
		if err != nil {
			return err
		}

		wrapped := grpc_middleware.WrapServerStream(stream)
		wrapped.WrappedContext = newCtx
		return handler(srv, wrapped)
	}
}

// GetUIDByCtx 从 Context 中提取用户 ID（uint64 格式）。
// 该方法直接从 metadata 中解析 Authorization 头，重新解析 Token，
// 适用于没有使用本拦截器或需要独立获取 UID 的场景。
func GetUIDByCtx(ctx context.Context) (uid uint64, err error) {
	var claims *jwt.Claims
	authorization := metautils.ExtractIncoming(ctx).Get("Authorization")
	if len(authorization) > 6 {
		token := authorization[7:] // 去除 "Bearer " 前缀
		claims, err = jwt.ParseToken(token)
		if err != nil {
			return uid, err
		}
		return utils.StrToUint64(claims.UID), err
	}
	return 0, errors.New("no authorization")
}
