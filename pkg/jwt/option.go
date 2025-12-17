package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// HS256 Method
	HS256 = jwt.SigningMethodHS256
	// HS384 Method
	HS384 = jwt.SigningMethodHS384
	// HS512 Method
	HS512 = jwt.SigningMethodHS512
)

var (
	defaultSigningKey    = []byte("zaq12wsxmko0") // default key
	defaultSigningMethod = HS256                  // default HS256
	defaultExpire        = 24 * time.Hour         // default expiration
	defaultIssuer        = ""
)

type options struct {
	signingKey    []byte
	expire        time.Duration
	issuer        string
	signingMethod *jwt.SigningMethodHMAC

	// Additional RegisteredClaims fields
	subject   string
	audience  []string
	id        string
	notBefore time.Time
}

func defaultOptions() *options {
	return &options{
		signingKey:    defaultSigningKey,
		signingMethod: defaultSigningMethod,
		expire:        defaultExpire,
		issuer:        defaultIssuer,
	}
}

// Option set the jwt options.
type Option func(*options)

func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithSigningKey set signing key value
func WithSigningKey(key string) Option {
	return func(o *options) {
		o.signingKey = []byte(key)
	}
}

// WithSigningMethod set signing method value
func WithSigningMethod(sm *jwt.SigningMethodHMAC) Option {
	return func(o *options) {
		o.signingMethod = sm
	}
}

// WithExpire set expire value
func WithExpire(d time.Duration) Option {
	return func(o *options) {
		o.expire = d
	}
}

// WithIssuer set issuer value
func WithIssuer(issuer string) Option {
	return func(o *options) {
		o.issuer = issuer
	}
}

// WithSubject set subject value
func WithSubject(subject string) Option {
	return func(o *options) {
		o.subject = subject
	}
}

// WithAudience set audience value
func WithAudience(audience []string) Option {
	return func(o *options) {
		o.audience = audience
	}
}

// WithID set JWT ID value
func WithID(id string) Option {
	return func(o *options) {
		o.id = id
	}
}

// WithNotBefore set not before value
func WithNotBefore(notBefore time.Time) Option {
	return func(o *options) {
		o.notBefore = notBefore
	}
}

var (
	errSignature = errors.New("signature failure")
	errInit      = errors.New("not yet initialized jwt, usage 'jwt.Init()'")
)
