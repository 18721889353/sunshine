package config

import (
	"sync/atomic"
	"time"

	v5 "github.com/golang-jwt/jwt/v5"

	"github.com/18721889353/sunshine/pkg/jwt"
)

// jwtManager 应用侧唯一的默认 JWT Manager 实例。
// 由 InitJwt 创建并持有，消费方（middleware/interceptor/gows）通过 JwtManager() 取出后注入；
// 热更新对同一实例调 Reload，注入的指针永不变，天然热更。
var jwtManager atomic.Pointer[jwt.Manager]

// InitJwt 按当前配置（Get().Jwt）创建默认 JWT Manager 并持有。
// 缺少签名密钥时返回 ErrSigningKeyNotConfigured（fail fast，启动方负责 panic）。
// 可重复调用：已有实例时等价于按当前配置 Reload。
func InitJwt() error {
	return applyJwtConfig(Get().Jwt)
}

// JwtManager 返回当前持有的默认 JWT Manager。
// InitJwt 未调用（如 OpenJwt 关闭）时返回 nil；
// 消费方已做 nil 守卫（返回 ErrNotInitialized / 401 拒绝），不会 panic。
func JwtManager() *jwt.Manager {
	return jwtManager.Load()
}

// applyJwtConfig 用指定 JWT 配置创建或 Reload 默认实例。
// InitJwt（启动）与 reloadJwtConfig（热更新）共用：
// 热更新回调先于 Set(newCfg) 执行，必须传入 newCfg.Jwt 而不能读 Get()。
// 创建/Reload 失败时不写入新配置，调用方决定 fail fast 还是保留旧配置。
func applyJwtConfig(j Jwt) error {
	opts := jwtOptionsFromCfg(j)
	mgr := jwtManager.Load()
	if mgr == nil {
		m, err := jwt.New(opts...)
		if err != nil {
			return err
		}
		jwtManager.Store(m)
		return nil
	}
	return mgr.Reload(opts...)
}

// jwtOptionsFromCfg 将 Jwt 配置段转换为 jwt.Option 列表，
// 抽出公共逻辑以消除启动初始化与热更新两处重复的 signingMethod 选择样板。
func jwtOptionsFromCfg(j Jwt) []jwt.Option {
	// 根据配置选择签名算法，支持 HS256/HS384/HS512
	var sm *v5.SigningMethodHMAC
	switch j.SigningMethod {
	case "HS256":
		sm = jwt.HS256
	case "HS384":
		sm = jwt.HS384
	default:
		sm = jwt.HS512
	}
	return []jwt.Option{
		jwt.WithExpire(time.Minute * time.Duration(j.Expire)),
		jwt.WithSigningKey(j.SigningKey),
		jwt.WithSigningMethod(sm),
		jwt.WithIssuer(j.Issuer),
	}
}
