package jwt

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateTokenSuccess 验证签发的标准 token 能被解析，且 uid、name、自定义字段与注册声明原样保留。
func TestGenerateTokenSuccess(t *testing.T) {
	initTestJWT(t)
	Init(
		WithSigningKey(testSigningKey),
		WithExpire(time.Hour),
		WithIssuer("sunshine-test"),
		WithSubject("test_subject"),
		WithAudience([]string{"test_audience"}),
		WithID("test_id"),
		WithNotBefore(time.Now().Add(-time.Minute)),
	)

	token, err := GenerateToken("10001", "sunshine", map[string]any{"role": "admin"})
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := ParseToken(token)
	require.NoError(t, err)

	assert.Equal(t, "10001", claims.UID)
	assert.Equal(t, "sunshine", claims.Name)
	assert.Equal(t, "admin", claims.Fields["role"])
	assert.Equal(t, "sunshine-test", claims.Issuer)
	assert.Equal(t, "test_subject", claims.Subject)
	assert.Equal(t, "test_id", claims.ID)
	assert.Equal(t, jwt.ClaimStrings{"test_audience"}, claims.Audience)
	assert.NotNil(t, claims.NotBefore)
}

// TestGenerateTokenDefaultFields 验证不传自定义字段时 Fields 为空 map，name 为空串也正常签发。
func TestGenerateTokenDefaultFields(t *testing.T) {
	initTestJWT(t)

	token, err := GenerateToken("10001", "")
	require.NoError(t, err)

	claims, err := ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "10001", claims.UID)
	assert.Empty(t, claims.Name)
	assert.NotNil(t, claims.Fields)
	assert.Empty(t, claims.Fields)
}

// TestGenerateTokenOmitsZeroNotBefore 验证未设置 WithNotBefore 时 token 不写入 nbf 字段。
func TestGenerateTokenOmitsZeroNotBefore(t *testing.T) {
	initTestJWT(t)

	token, err := GenerateToken("10001", "sunshine")
	require.NoError(t, err)

	claims, err := ParseToken(token)
	require.NoError(t, err)
	// 零值不写入 nbf：解析后为 nil，而不是零值时间戳（-62135596800）
	assert.Nil(t, claims.NotBefore)
}

// TestGenerateTokenWithoutInit 验证未调用 Init 时签发标准 token 返回 ErrNotInitialized。
func TestGenerateTokenWithoutInit(t *testing.T) {
	optStore.Store(nil)
	t.Cleanup(func() { Init() })

	_, err := GenerateToken("10001", "sunshine")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

// TestParseTokenNotInitialized 验证未调用 Init 时解析标准 token 返回 ErrNotInitialized。
func TestParseTokenNotInitialized(t *testing.T) {
	optStore.Store(nil)
	t.Cleanup(func() { Init() })

	_, err := ParseToken("any.token.string")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

// TestParseTokenInvalidFormat 验证格式非法的 token 解析失败且不返回 claims。
func TestParseTokenInvalidFormat(t *testing.T) {
	initTestJWT(t)

	testCases := []string{"", "abc", "xxx.xxx.xxx"}
	for _, tokenString := range testCases {
		claims, err := ParseToken(tokenString)
		assert.Error(t, err, "token %q 应解析失败", tokenString)
		assert.Nil(t, claims)
	}
}

// TestParseTokenTamperedSignature 验证签名被篡改或密钥不匹配的 token 解析失败。
func TestParseTokenTamperedSignature(t *testing.T) {
	initTestJWT(t)

	token, err := GenerateToken("10001", "sunshine")
	require.NoError(t, err)

	t.Run("签名被追加篡改", func(t *testing.T) {
		claims, err := ParseToken(token + "xxx")
		assert.Error(t, err)
		assert.Nil(t, claims)
	})

	t.Run("密钥不匹配", func(t *testing.T) {
		// 用另一把密钥初始化后解析原 token，必须校验失败
		Init(WithSigningKey("another-signing-key-for-mismatch"))
		claims, err := ParseToken(token)
		assert.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
		assert.Nil(t, claims)
	})
}

// TestParseTokenExpired 验证过期 token 解析返回 ErrTokenExpired。
// 用负的过期时长直接签发「已过期」token，无需 time.Sleep 等待真实过期，测试即时且无 flaky。
func TestParseTokenExpired(t *testing.T) {
	initTestJWT(t)
	Init(WithSigningKey(testSigningKey), WithExpire(-time.Minute))

	token, err := GenerateToken("10001", "sunshine")
	require.NoError(t, err)

	claims, err := ParseToken(token)
	assert.True(t, errors.Is(err, ErrTokenExpired))
	assert.Nil(t, claims)
}

// TestRefreshTokenExtendsExpiry 验证刷新保留业务字段并按当前配置重新签发（有效期与注册声明更新）。
func TestRefreshTokenExtendsExpiry(t *testing.T) {
	initTestJWT(t)
	Init(WithSigningKey(testSigningKey), WithExpire(time.Hour), WithIssuer("old-issuer"))

	token, err := GenerateToken("10001", "sunshine", map[string]any{"role": "admin"})
	require.NoError(t, err)
	oldClaims, err := ParseToken(token)
	require.NoError(t, err)

	// 刷新时切换配置，验证新 token 反映的是「刷新时刻的配置」
	Init(WithSigningKey(testSigningKey), WithExpire(2*time.Hour), WithIssuer("new-issuer"))
	newToken, err := RefreshToken(token)
	require.NoError(t, err)
	assert.NotEqual(t, token, newToken)

	newClaims, err := ParseToken(newToken)
	require.NoError(t, err)
	assert.Equal(t, oldClaims.UID, newClaims.UID)
	assert.Equal(t, oldClaims.Name, newClaims.Name)
	assert.Equal(t, "admin", newClaims.Fields["role"])
	assert.Equal(t, "new-issuer", newClaims.Issuer)
	require.NotNil(t, newClaims.ExpiresAt)
	assert.True(t, newClaims.ExpiresAt.Time.After(oldClaims.ExpiresAt.Time))
}

// TestRefreshTokenNotInitialized 验证未调用 Init 时刷新标准 token 返回 ErrNotInitialized。
func TestRefreshTokenNotInitialized(t *testing.T) {
	optStore.Store(nil)
	t.Cleanup(func() { Init() })

	_, err := RefreshToken("any.token.string")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

// TestRefreshTokenExpiredSource 验证已过期的原 token 无法刷新（刷新前会先解析校验）。
func TestRefreshTokenExpiredSource(t *testing.T) {
	initTestJWT(t)
	Init(WithSigningKey(testSigningKey), WithExpire(-time.Minute))

	token, err := GenerateToken("10001", "sunshine")
	require.NoError(t, err)

	_, err = RefreshToken(token)
	assert.True(t, errors.Is(err, ErrTokenExpired))
}

// TestGenerateCustomTokenRoundTrip 验证自定义字段 token 签发后解析，各类型字段可原样取回。
func TestGenerateCustomTokenRoundTrip(t *testing.T) {
	initTestJWT(t)

	fields := KV{
		"name":  "sunshine",
		"age":   10,
		"id":    uint64(20),
		"vip":   true,
		"score": 9.5,
	}
	token, err := GenerateCustomToken(fields)
	require.NoError(t, err)

	claims, err := ParseCustomToken(token)
	require.NoError(t, err)

	t.Run("字符串字段", func(t *testing.T) {
		v, ok := claims.GetString("name")
		assert.True(t, ok)
		assert.Equal(t, "sunshine", v)
	})

	t.Run("整数字段", func(t *testing.T) {
		// 数字经 token JSON 往返后为 float64，GetInt 负责归一化
		v, ok := claims.GetInt("age")
		assert.True(t, ok)
		assert.Equal(t, 10, v)
	})

	t.Run("无符号整数字段", func(t *testing.T) {
		v, ok := claims.GetUint64("id")
		assert.True(t, ok)
		assert.Equal(t, uint64(20), v)
	})

	t.Run("布尔字段", func(t *testing.T) {
		v, ok := claims.Get("vip")
		assert.True(t, ok)
		assert.Equal(t, true, v)
	})

	t.Run("缺失字段", func(t *testing.T) {
		_, ok := claims.GetString("not-exist")
		assert.False(t, ok)
	})
}

// TestGenerateCustomTokenWithoutInit 验证未调用 Init 时签发自定义字段 token 返回 ErrNotInitialized。
func TestGenerateCustomTokenWithoutInit(t *testing.T) {
	optStore.Store(nil)
	t.Cleanup(func() { Init() })

	_, err := GenerateCustomToken(KV{"foo": "bar"})
	assert.ErrorIs(t, err, ErrNotInitialized)
}

// TestParseCustomTokenNotInitialized 验证未调用 Init 时解析自定义字段 token 返回 ErrNotInitialized。
func TestParseCustomTokenNotInitialized(t *testing.T) {
	optStore.Store(nil)
	t.Cleanup(func() { Init() })

	_, err := ParseCustomToken("any.token.string")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

// TestParseCustomTokenInvalidInput 验证格式非法与签名被篡改的自定义字段 token 解析失败。
func TestParseCustomTokenInvalidInput(t *testing.T) {
	initTestJWT(t)

	token, err := GenerateCustomToken(KV{"foo": "bar"})
	require.NoError(t, err)

	testCases := []string{"", "abc", "xxx.xxx.xxx", token + "xxx"}
	for _, tokenString := range testCases {
		claims, err := ParseCustomToken(tokenString)
		assert.Error(t, err, "token %q 应解析失败", tokenString)
		assert.Nil(t, claims)
	}
}

// TestRefreshCustomTokenKeepsFields 验证刷新自定义字段 token 保留 Fields 并按当前配置重新签发。
func TestRefreshCustomTokenKeepsFields(t *testing.T) {
	initTestJWT(t)
	Init(WithSigningKey(testSigningKey), WithExpire(time.Hour), WithIssuer("old-issuer"))

	token, err := GenerateCustomToken(KV{"foo": "bar"})
	require.NoError(t, err)

	Init(WithSigningKey(testSigningKey), WithExpire(2*time.Hour), WithIssuer("new-issuer"))
	newToken, err := RefreshCustomToken(token)
	require.NoError(t, err)
	assert.NotEqual(t, token, newToken)

	claims, err := ParseCustomToken(newToken)
	require.NoError(t, err)
	v, ok := claims.GetString("foo")
	assert.True(t, ok)
	assert.Equal(t, "bar", v)
	assert.Equal(t, "new-issuer", claims.Issuer)
}

// TestRefreshCustomTokenNotInitialized 验证未调用 Init 时刷新自定义字段 token 返回 ErrNotInitialized。
func TestRefreshCustomTokenNotInitialized(t *testing.T) {
	optStore.Store(nil)
	t.Cleanup(func() { Init() })

	_, err := RefreshCustomToken("any.token.string")
	assert.ErrorIs(t, err, ErrNotInitialized)
}

// TestCustomClaimsGettersNilFields 验证 Fields 为 nil 时各取值方法返回零值且 isExist 为 false。
func TestCustomClaimsGettersNilFields(t *testing.T) {
	claims := &CustomClaims{}

	_, ok := claims.Get("any")
	assert.False(t, ok)
	_, ok = claims.GetString("any")
	assert.False(t, ok)
	_, ok = claims.GetInt("any")
	assert.False(t, ok)
	_, ok = claims.GetUint64("any")
	assert.False(t, ok)
}

// TestCustomClaimsGettersTypeMismatch 验证字段类型与取值方法不匹配时返回零值且 isExist 为 false。
func TestCustomClaimsGettersTypeMismatch(t *testing.T) {
	claims := &CustomClaims{Fields: KV{"name": "sunshine", "age": 10}}

	_, ok := claims.GetInt("name")
	assert.False(t, ok)
	_, ok = claims.GetString("age")
	assert.False(t, ok)
	// GetUint64 的类型集合为 float64/uint64，内存对象中的 int 不在其中
	_, ok = claims.GetUint64("age")
	assert.False(t, ok)
}

// TestInitConcurrentWithParse 验证 Init 热更新与并发签发/解析/刷新之间无数据竞争，
// 重点覆盖 Refresh 的「单次 Load 配置快照」在热更新并发下的一致性。
// 结果判定全部走断言而非睡眠等待；竞态检测需在支持 cgo 的环境用 -race 复核（见 README 说明）。
func TestInitConcurrentWithParse(t *testing.T) {
	initTestJWT(t)

	const (
		goroutinePairs = 8
		loopCount      = 200
	)

	var wg sync.WaitGroup
	for i := 0; i < goroutinePairs; i++ {
		wg.Add(2)
		// 模拟配置热更新线程
		go func() {
			defer wg.Done()
			for j := 0; j < loopCount; j++ {
				Init(WithSigningKey(testSigningKey), WithExpire(time.Hour))
			}
		}()
		// 模拟请求线程：签发 → 解析 → 刷新（两条刷新链路都纳入并发覆盖）
		go func() {
			defer wg.Done()
			for j := 0; j < loopCount; j++ {
				token, err := GenerateToken("10001", "sunshine")
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := ParseToken(token); err != nil {
					t.Error(err)
					return
				}
				if _, err := RefreshToken(token); err != nil {
					t.Error(err)
					return
				}
				custom, err := GenerateCustomToken(KV{"foo": "bar"})
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := RefreshCustomToken(custom); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestInitWithoutOptionsResetsToNoKey 验证不传选项的 Init 恢复默认配置：
// 恢复 HS256、24 小时、空签发者，且签名密钥被清空（安全设计，不保留旧密钥也不回退默认密钥）。
func TestInitWithoutOptionsResetsToNoKey(t *testing.T) {
	initTestJWT(t)
	Init(WithSigningKey("custom-key"), WithExpire(time.Minute), WithIssuer("custom"))

	Init()
	o := optStore.Load()
	require.NotNil(t, o)
	assert.Empty(t, o.signingKey, "Init() 必须清空密钥，不得保留旧密钥或回退默认密钥")
	assert.Equal(t, HS256, o.signingMethod)
	assert.Equal(t, 24*time.Hour, o.expire)
	assert.Empty(t, o.issuer)

	// 无密钥状态下所有入口 fail loud，返回 ErrSigningKeyNotConfigured
	_, err := GenerateToken("10001", "sunshine")
	assert.ErrorIs(t, err, ErrSigningKeyNotConfigured)
	_, err = ParseToken("any.token.string")
	assert.ErrorIs(t, err, ErrSigningKeyNotConfigured)
	_, err = GenerateCustomToken(KV{"foo": "bar"})
	assert.ErrorIs(t, err, ErrSigningKeyNotConfigured)
	_, err = ParseCustomToken("any.token.string")
	assert.ErrorIs(t, err, ErrSigningKeyNotConfigured)
}

// TestRequireSigningKeyPanicsWhenMissing 验证 RequireSigningKey 下密钥缺失时 Init 直接 panic（fail fast）。
func TestRequireSigningKeyPanicsWhenMissing(t *testing.T) {
	initTestJWT(t)

	assert.PanicsWithError(t, ErrSigningKeyNotConfigured.Error(), func() {
		Init(RequireSigningKey())
	})
	// 空串密钥同样视为未配置
	assert.PanicsWithError(t, ErrSigningKeyNotConfigured.Error(), func() {
		Init(RequireSigningKey(), WithSigningKey(""))
	})

	// panic 时不写入新配置，原有配置保持可用
	token, err := GenerateToken("10001", "sunshine")
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

// TestRequireSigningKeyPassesWithKey 验证 RequireSigningKey 在密钥已配置时不 panic。
func TestRequireSigningKeyPassesWithKey(t *testing.T) {
	initTestJWT(t)

	assert.NotPanics(t, func() {
		Init(RequireSigningKey(), WithSigningKey(testSigningKey))
	})
	token, err := GenerateToken("10001", "sunshine")
	require.NoError(t, err)
	require.NotEmpty(t, token)
}

// TestParseTokenRejectsUnconfiguredMethod 验证解析时显式限定签名算法：
// 同一密钥下用 HS384 签发的 token，在配置为 HS256 时解析被拒绝（WithValidMethods 生效）。
func TestParseTokenRejectsUnconfiguredMethod(t *testing.T) {
	initTestJWT(t) // 默认算法 HS256

	signed, err := jwt.NewWithClaims(HS384, Claims{
		UID:  "10001",
		Name: "sunshine",
	}).SignedString([]byte(testSigningKey))
	require.NoError(t, err)

	claims, err := ParseToken(signed)
	assert.Error(t, err, "算法不在允许列表的 token 应解析失败")
	assert.Nil(t, claims)
}

// TestGenerateTokenCopiesFields 验证签发时对传入 map 做浅拷贝：
// 签发后调用方继续修改原 map，不影响已签发 token 的内容。
func TestGenerateTokenCopiesFields(t *testing.T) {
	initTestJWT(t)

	fields := map[string]any{"role": "admin"}
	token, err := GenerateToken("10001", "sunshine", fields)
	require.NoError(t, err)

	fields["role"] = "guest" // 签发后篡改原 map
	claims, err := ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "admin", claims.Fields["role"], "已签发 token 不受原 map 后续修改影响")
}

// TestGenerateCustomTokenCopiesKV 验证自定义字段签发时对传入 kv 做浅拷贝（同 GenerateToken 口径）。
func TestGenerateCustomTokenCopiesKV(t *testing.T) {
	initTestJWT(t)

	kv := KV{"role": "admin"}
	token, err := GenerateCustomToken(kv)
	require.NoError(t, err)

	kv["role"] = "guest"
	claims, err := ParseCustomToken(token)
	require.NoError(t, err)
	v, ok := claims.GetString("role")
	require.True(t, ok)
	assert.Equal(t, "admin", v)
}

// signTokenWithClaims 用当前测试配置的签名算法与密钥手工签发 token，
// 独立于写入端选项（WithIssuer / WithAudience），专用于验证解析侧校验行为。
// 算法与密钥从 optStore 读取而非硬编码：若未来默认算法变更，
// 本 helper 与解析侧自动对齐，用例失败不会指向错误的原因。
func signTokenWithClaims(t *testing.T, registered jwt.RegisteredClaims) string {
	t.Helper()
	o := optStore.Load()
	require.NotNil(t, o, "signTokenWithClaims 依赖 initTestJWT 已初始化全局配置")
	signed, err := jwt.NewWithClaims(o.signingMethod, Claims{
		UID:    "10001",
		Name:   "sunshine",
		Fields: map[string]any{},
		CustomRegisteredClaims: CustomRegisteredClaims{
			RegisteredClaims: registered,
		},
	}).SignedString(o.signingKey)
	require.NoError(t, err)
	return signed
}

// TestParseTokenIssuerValidation 验证 WithExpectedIssuer 的 iss 校验：
// 未配置期望时不校验（兼容旧行为），配置后拒绝 iss 不匹配的 token（多租户隔离）。
func TestParseTokenIssuerValidation(t *testing.T) {
	initTestJWT(t)

	token := signTokenWithClaims(t, jwt.RegisteredClaims{
		Issuer:    "attacker",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	t.Run("未配置期望时兼容不校验", func(t *testing.T) {
		Init(WithSigningKey(testSigningKey))
		claims, err := ParseToken(token)
		require.NoError(t, err)
		assert.Equal(t, "attacker", claims.Issuer)
	})

	t.Run("配置期望后拒绝不匹配的 iss", func(t *testing.T) {
		Init(WithSigningKey(testSigningKey), WithExpectedIssuer("sunshine"))
		claims, err := ParseToken(token)
		assert.ErrorIs(t, err, jwt.ErrTokenInvalidIssuer)
		assert.Nil(t, claims)
	})

	t.Run("iss 匹配期望时正常解析", func(t *testing.T) {
		matched := signTokenWithClaims(t, jwt.RegisteredClaims{
			Issuer:    "sunshine",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		})
		Init(WithSigningKey(testSigningKey), WithExpectedIssuer("sunshine"))
		claims, err := ParseToken(matched)
		require.NoError(t, err)
		assert.Equal(t, "sunshine", claims.Issuer)
	})
}

// TestParseTokenAudienceValidation 验证 WithExpectedAudience 的 aud 校验（同 iss 口径）。
func TestParseTokenAudienceValidation(t *testing.T) {
	initTestJWT(t)

	token := signTokenWithClaims(t, jwt.RegisteredClaims{
		Audience:  jwt.ClaimStrings{"service-b"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	t.Run("配置期望后拒绝不匹配的 aud", func(t *testing.T) {
		Init(WithSigningKey(testSigningKey), WithExpectedAudience("service-a"))
		claims, err := ParseToken(token)
		assert.ErrorIs(t, err, jwt.ErrTokenInvalidAudience)
		assert.Nil(t, claims)
	})

	t.Run("aud 匹配期望时正常解析", func(t *testing.T) {
		Init(WithSigningKey(testSigningKey), WithExpectedAudience("service-b"))
		claims, err := ParseToken(token)
		require.NoError(t, err)
		require.NotNil(t, claims)
	})
}

// TestParseTokenLeewayAllowsSlightlyExpired 验证 WithLeeway 时钟偏差容忍：
// 过期 1 分钟的 token 在 2 分钟 leeway 内仍可解析（分布式节点时钟偏差场景），
// 无 leeway 时同一 token 被拒绝由 TestParseTokenExpired 覆盖。
func TestParseTokenLeewayAllowsSlightlyExpired(t *testing.T) {
	initTestJWT(t)
	Init(WithSigningKey(testSigningKey), WithExpire(-time.Minute))

	token, err := GenerateToken("10001", "sunshine")
	require.NoError(t, err)

	Init(WithSigningKey(testSigningKey), WithExpire(-time.Minute), WithLeeway(2*time.Minute))
	claims, err := ParseToken(token)
	require.NoError(t, err, "leeway 覆盖范围内的轻微过期应被容忍")
	assert.Equal(t, "10001", claims.UID)
}
