package jwt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDefaultOptionsValues 验证默认配置与 README「Option 列表」记录的默认值一致。
// 安全设计：默认不含签名密钥，本包刻意不提供公开的默认密钥。
func TestDefaultOptionsValues(t *testing.T) {
	o := defaultOptions()
	assert.Empty(t, o.signingKey, "默认必须不含签名密钥（安全设计）")
	assert.False(t, o.requireSigningKey)
	assert.Equal(t, HS256, o.signingMethod)
	assert.Equal(t, 24*time.Hour, o.expire)
	assert.Empty(t, o.issuer)
	assert.Empty(t, o.subject)
	assert.Empty(t, o.audience)
	assert.Empty(t, o.id)
	assert.True(t, o.notBefore.IsZero())
}

// TestOptionsApplyLastWins 验证同名配置多次设置时以最后一次为准（最后赋值胜出）。
func TestOptionsApplyLastWins(t *testing.T) {
	o := defaultOptions()
	o.apply(
		WithSigningKey("first-key"),
		WithExpire(time.Minute),
		WithSigningKey("second-key"),
		WithExpire(2*time.Minute),
	)
	assert.Equal(t, "second-key", string(o.signingKey))
	assert.Equal(t, 2*time.Minute, o.expire)
}

// TestOptionsApplyEmptyKeepsDefaults 验证不传配置项时保留默认值。
func TestOptionsApplyEmptyKeepsDefaults(t *testing.T) {
	o := defaultOptions()
	o.apply()
	assert.Empty(t, o.signingKey)
	assert.Equal(t, defaultSigningMethod, o.signingMethod)
	assert.Equal(t, defaultExpire, o.expire)
}

// TestFinalizeNilSigningMethodFallsBack 验证 finalize 对 signingMethod 为 nil 的防御：
// defaultOptions 总是设置默认算法，此路径仅在未来新增 options 构造路径遗漏默认值时可达；
// finalize 应回退到默认算法并正常预计算白名单，而非 panic。
func TestFinalizeNilSigningMethodFallsBack(t *testing.T) {
	o := &options{} // 不经过 defaultOptions，signingMethod 为 nil
	o.finalize()

	assert.Equal(t, defaultSigningMethod, o.signingMethod, "signingMethod 应回退到默认算法")
	assert.Equal(t, []string{defaultSigningMethod.Alg()}, o.validMethods, "白名单应回退后的算法正常预计算")
}

// TestWithSigningKeyEmptyKeepsOldValue 验证空串密钥被视为未设置：
// 不覆盖已有密钥，避免配置中心返回空值时把已有密钥静默清空。
func TestWithSigningKeyEmptyKeepsOldValue(t *testing.T) {
	o := defaultOptions()
	o.apply(WithSigningKey("real-key"), WithSigningKey(""))
	assert.Equal(t, "real-key", string(o.signingKey))

	// 从未设置过密钥时，空串同样不产生密钥（fail-loud 路径不变）
	o = defaultOptions()
	o.apply(WithSigningKey(""))
	assert.Empty(t, o.signingKey)
}

// TestRequireSigningKeySetsFlag 验证 RequireSigningKey 打开 New/Reload 阶段的密钥强校验标志。
func TestRequireSigningKeySetsFlag(t *testing.T) {
	o := defaultOptions()
	assert.False(t, o.requireSigningKey)
	o.apply(RequireSigningKey())
	assert.True(t, o.requireSigningKey)
}

// TestWithSigningKeySetsKey 验证 WithSigningKey 将密钥写入配置。
func TestWithSigningKeySetsKey(t *testing.T) {
	o := new(options)
	o.apply(WithSigningKey("key"))
	assert.Equal(t, "key", string(o.signingKey))
}

// TestWithSigningMethodSetsMethod 验证 WithSigningMethod 将签名算法写入配置。
func TestWithSigningMethodSetsMethod(t *testing.T) {
	o := new(options)
	o.apply(WithSigningMethod(HS384))
	assert.Equal(t, HS384, o.signingMethod)
}

// TestWithSigningMethodNilIgnored 验证传入 nil 签名算法时被忽略，保留先前值而非写入 nil。
func TestWithSigningMethodNilIgnored(t *testing.T) {
	o := defaultOptions()
	o.apply(WithSigningMethod(nil))
	assert.Equal(t, HS256, o.signingMethod)

	o.apply(WithSigningMethod(HS512), WithSigningMethod(nil))
	assert.Equal(t, HS512, o.signingMethod)
}

// TestWithExpireSetsDuration 验证 WithExpire 将有效期写入配置。
func TestWithExpireSetsDuration(t *testing.T) {
	o := new(options)
	o.apply(WithExpire(time.Second * 3))
	assert.Equal(t, time.Second*3, o.expire)
}

// TestWithIssuerSetsIssuer 验证 WithIssuer 将签发者写入配置。
func TestWithIssuerSetsIssuer(t *testing.T) {
	o := new(options)
	o.apply(WithIssuer("issuer"))
	assert.Equal(t, "issuer", o.issuer)
}

// TestWithSubjectSetsSubject 验证 WithSubject 将主题写入配置。
func TestWithSubjectSetsSubject(t *testing.T) {
	o := new(options)
	o.apply(WithSubject("subject"))
	assert.Equal(t, "subject", o.subject)
}

// TestWithAudienceSetsAudience 验证 WithAudience 将受众列表写入配置。
func TestWithAudienceSetsAudience(t *testing.T) {
	audience := []string{"aud1", "aud2"}
	o := new(options)
	o.apply(WithAudience(audience))
	assert.Equal(t, audience, o.audience)
}

// TestWithAudienceCopiesSlice 验证入参切片内部拷贝：
// 调用方后续修改原切片不影响已生效的配置（与签发入参 map 的浅拷贝口径一致）。
func TestWithAudienceCopiesSlice(t *testing.T) {
	aud := []string{"svc-a", "svc-b"}
	o := new(options)
	o.apply(WithAudience(aud))

	aud[0] = "attacker" // apply 后篡改原切片
	assert.Equal(t, []string{"svc-a", "svc-b"}, o.audience, "配置不得受原切片后续修改影响")

	// 空切片重置为 nil（不写入 aud 字段）
	o.apply(WithAudience([]string{}))
	assert.Nil(t, o.audience)
}

// TestWithExpireNonPositiveAllowed 验证 0 / 负值有效期被有意接受
// （构造已过期 token 的测试手段，语义见 WithExpire 注释）。
func TestWithExpireNonPositiveAllowed(t *testing.T) {
	o := new(options)
	o.apply(WithExpire(0))
	assert.Equal(t, time.Duration(0), o.expire)

	o.apply(WithExpire(-time.Minute))
	assert.Equal(t, -time.Minute, o.expire)
}

// TestWithExpectedIssuerAndAudience 验证解析侧 iss/aud 校验选项的默认关闭、
// 显式设置、入参拷贝与空参重置语义。
func TestWithExpectedIssuerAndAudience(t *testing.T) {
	o := defaultOptions()
	assert.Empty(t, o.expectedIssuer, "默认不校验 iss")
	assert.Nil(t, o.expectedAudience, "默认不校验 aud")

	aud := []string{"svc-a"}
	o.apply(WithExpectedIssuer("sunshine"), WithExpectedAudience(aud...))
	assert.Equal(t, "sunshine", o.expectedIssuer)
	assert.Equal(t, []string{"svc-a"}, o.expectedAudience)

	aud[0] = "attacker" // 展开前的原切片被修改，配置不受影响
	assert.Equal(t, []string{"svc-a"}, o.expectedAudience, "入参切片必须内部拷贝")

	o.apply(WithExpectedAudience()) // 空参重置为不校验
	assert.Nil(t, o.expectedAudience)
}

// TestWithLeewaySetsDuration 验证 leeway 正值设置、非正值忽略的语义。
func TestWithLeewaySetsDuration(t *testing.T) {
	o := new(options)
	o.apply(WithLeeway(30 * time.Second))
	assert.Equal(t, 30*time.Second, o.leeway)

	o.apply(WithLeeway(0), WithLeeway(-time.Second))
	assert.Equal(t, 30*time.Second, o.leeway, "非正值应被忽略，保留当前值")
}

// TestWithIDSetsID 验证 WithID 将 JWT ID 写入配置。
func TestWithIDSetsID(t *testing.T) {
	o := new(options)
	o.apply(WithID("id-1"))
	assert.Equal(t, "id-1", o.id)
}

// TestWithNotBeforeSetsTime 验证 WithNotBefore 将生效时间写入配置。
func TestWithNotBeforeSetsTime(t *testing.T) {
	notBefore := time.Now().Add(-time.Hour)
	o := new(options)
	o.apply(WithNotBefore(notBefore))
	assert.True(t, o.notBefore.Equal(notBefore))
}

// TestSigningMethodConstants 验证导出的签名算法常量非空且互不相同。
func TestSigningMethodConstants(t *testing.T) {
	assert.NotNil(t, HS256)
	assert.NotNil(t, HS384)
	assert.NotNil(t, HS512)
	assert.NotEqual(t, HS256.Alg(), HS384.Alg())
	assert.NotEqual(t, HS384.Alg(), HS512.Alg())
}
