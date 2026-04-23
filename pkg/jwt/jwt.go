// Package jwt is token generation and validation.
package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrTokenExpired expired
var ErrTokenExpired = jwt.ErrTokenExpired

var opt *options

// Init initialize jwt
func Init(opts ...Option) {
	o := defaultOptions()
	o.apply(opts...)
	opt = o
}

// CustomRegisteredClaims custom registered claims
type CustomRegisteredClaims struct {
	jwt.RegisteredClaims
	Subject any `json:"sub,omitempty"`
}

// Claims standard claims, include uid, name, and CustomRegisteredClaims
type Claims struct {
	UID    string         `json:"uid"`
	Name   string         `json:"name"`
	Fields map[string]any `json:"data"`
	CustomRegisteredClaims
}

// GenerateToken generate token by uid and name, use universal Claims
func GenerateToken(uid string, name string, kvs ...map[string]any) (string, error) {
	if opt == nil {
		return "", errInit
	}

	nameVal := ""
	if len(name) > 0 {
		nameVal = name
	}
	field := make(map[string]any)
	if len(kvs) > 0 {
		field = kvs[0]
	}
	// Prepare RegisteredClaims with all customizable fields
	registeredClaims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(opt.expire)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Subject:   opt.subject,
		Issuer:    opt.issuer,
		Audience:  opt.audience,
		ID:        opt.id,
		NotBefore: jwt.NewNumericDate(opt.notBefore),
	}

	claims := Claims{
		UID:    uid,
		Name:   nameVal,
		Fields: field,
		CustomRegisteredClaims: CustomRegisteredClaims{
			RegisteredClaims: registeredClaims,
			Subject:          opt.subject,
		},
	}

	token := jwt.NewWithClaims(opt.signingMethod, claims)
	return token.SignedString(opt.signingKey)
}

// ParseToken parse token, return universal Claims
func ParseToken(tokenString string) (*Claims, error) {
	if opt == nil {
		return nil, errInit
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(_ *jwt.Token) (interface{}, error) {
		return opt.signingKey, nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errSignature
}

// RefreshToken refresh token
func RefreshToken(tokenString string) (string, error) {
	claims, err := ParseToken(tokenString)
	if err != nil {
		return "", err
	}

	// Prepare RegisteredClaims with all customizable fields
	registeredClaims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(opt.expire)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Issuer:    opt.issuer,
		Subject:   opt.subject,
		Audience:  opt.audience,
		ID:        opt.id,
		NotBefore: jwt.NewNumericDate(opt.notBefore),
	}

	claims.CustomRegisteredClaims.RegisteredClaims = registeredClaims

	token := jwt.NewWithClaims(opt.signingMethod, claims)
	return token.SignedString(opt.signingKey)
}

// -------------------------------------------------------------------------------------------

// KV map type
type KV = map[string]any

// CustomClaims custom fields claims
type CustomClaims struct {
	Fields KV `json:"data"`
	CustomRegisteredClaims
}

// Get custom field value by key, if not found, return false
func (c *CustomClaims) Get(key string) (val interface{}, isExist bool) {
	if c.Fields == nil {
		return nil, false
	}
	val, isExist = c.Fields[key]
	return val, isExist
}

// GetString custom field value by key, if not found, return false
func (c *CustomClaims) GetString(key string) (string, bool) {
	val, isExist := c.Get(key)
	if isExist {
		str, ok := val.(string)
		return str, ok
	}
	return "", false
}

// GetInt custom field value by key, if not found, return false
func (c *CustomClaims) GetInt(key string) (int, bool) {
	val, isExist := c.Get(key)
	if isExist {
		if v, ok := val.(float64); ok {
			return int(v), true
		}
		if v, ok := val.(int); ok {
			return v, true
		}
	}
	return 0, false
}

// GetUint64 custom field value by key, if not found, return false
func (c *CustomClaims) GetUint64(key string) (uint64, bool) {
	val, isExist := c.Get(key)
	if isExist {
		if v, ok := val.(float64); ok {
			return uint64(v), true
		}
		if v, ok := val.(uint64); ok {
			return v, true
		}
	}
	return 0, false
}

// GenerateCustomToken generate token by custom fields, use CustomClaims
func GenerateCustomToken(kv map[string]interface{}) (string, error) {
	if opt == nil {
		return "", errInit
	}

	// Prepare RegisteredClaims with all customizable fields
	registeredClaims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(opt.expire)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Issuer:    opt.issuer,
		Subject:   opt.subject,
		Audience:  opt.audience,
		ID:        opt.id,
		NotBefore: jwt.NewNumericDate(opt.notBefore),
	}

	claims := CustomClaims{
		Fields: kv,
		CustomRegisteredClaims: CustomRegisteredClaims{
			RegisteredClaims: registeredClaims,
			Subject:          opt.subject,
		},
	}

	token := jwt.NewWithClaims(opt.signingMethod, claims)
	return token.SignedString(opt.signingKey)
}

// ParseCustomToken parse token, return CustomClaims
func ParseCustomToken(tokenString string) (*CustomClaims, error) {
	if opt == nil {
		return nil, errInit
	}

	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(_ *jwt.Token) (interface{}, error) {
		return opt.signingKey, nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*CustomClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errSignature
}

// RefreshCustomToken refresh custom token
func RefreshCustomToken(tokenString string) (string, error) {
	claims, err := ParseCustomToken(tokenString)
	if err != nil {
		return "", err
	}

	// Prepare RegisteredClaims with all customizable fields
	registeredClaims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(opt.expire)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Issuer:    opt.issuer,
		Subject:   opt.subject,
		Audience:  opt.audience,
		ID:        opt.id,
		NotBefore: jwt.NewNumericDate(opt.notBefore),
	}

	claims.CustomRegisteredClaims.RegisteredClaims = registeredClaims

	token := jwt.NewWithClaims(opt.signingMethod, claims)
	return token.SignedString(opt.signingKey)
}
