package interceptor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/18721889353/sunshine/pkg/jwt"
)

// 本文件覆盖 WithJwtManager 实例注入的行为：
// 注入后 jwtVerify 解析成功、未注入（nil）时返回 Unauthenticated（fail loud，不 panic）。

// newJwtVerifyTestManager 创建 jwtVerify 测试专用的 jwt.Manager（固定测试密钥）。
func newJwtVerifyTestManager(t testing.TB) *jwt.Manager {
	t.Helper()
	mgr, err := jwt.New(jwt.WithSigningKey("TEST_ONLY_interceptor_test_signing_key"))
	require.NoError(t, err, "创建测试 Manager 失败")
	return mgr
}

// TestJwtVerifyWithInjectedManager 验证注入 Manager 后 jwtVerify 能解析合法 token，
// 并把标准 Claims 写入返回的 context。
func TestJwtVerifyWithInjectedManager(t *testing.T) {
	mgr := newJwtVerifyTestManager(t)
	token, err := mgr.GenerateToken("10001", "sunshine", map[string]any{"role": "admin"})
	require.NoError(t, err)
	require.Greater(t, len(token), 100, "测试 token 需满足拦截器最短长度校验")

	md := metadata.Pairs(headerAuthorize, authScheme+" "+token)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	// opt 传 nil 走默认标准 Claims 分支
	newCtx, err := jwtVerify(ctx, nil, mgr)
	require.NoError(t, err)

	claims, ok := GetJwtClaims(newCtx)
	require.True(t, ok, "返回的 context 中应携带标准 Claims")
	assert.Equal(t, "10001", claims.UID)
	assert.Equal(t, "admin", claims.Fields["role"])
}

// TestJwtVerifyNilManagerRejects 验证未注入 Manager（nil）时 jwtVerify
// 直接返回 Unauthenticated 状态错误，不 panic。
func TestJwtVerifyNilManagerRejects(t *testing.T) {
	_, err := jwtVerify(context.Background(), nil, nil)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok, "应返回 gRPC status 错误")
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Contains(t, st.Message(), "jwt manager not injected")
}
