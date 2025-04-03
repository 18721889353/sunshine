package middleware

import (
	"github.com/gin-contrib/cors"
	"time"

	"github.com/gin-gonic/gin"
)

// CorsOption set the CORS options.
type CorsOption func(*CorsConfig)

// CorsConfig holds the configuration for CORS middleware
type CorsConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           time.Duration
}

func defaultCorsConfig() *CorsConfig {
	return &CorsConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Authorization", "Content-Type", "Accept"},
		ExposeHeaders:    []string{"Content-Length", "text/plain", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
}
func (o *CorsConfig) apply(opts ...CorsOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithAllowOrigins sets the allowed origins.
func WithAllowOrigins(origins []string) CorsOption {
	return func(o *CorsConfig) {
		o.AllowOrigins = origins
	}
}

// WithAllowMethods sets the allowed methods.
func WithAllowMethods(methods []string) CorsOption {
	return func(o *CorsConfig) {
		o.AllowMethods = methods
	}
}

// WithAllowHeaders sets the allowed headers.
func WithAllowHeaders(headers []string) CorsOption {
	return func(o *CorsConfig) {
		o.AllowHeaders = headers
	}
}

// WithExposeHeaders sets the exposed headers.
func WithExposeHeaders(headers []string) CorsOption {
	return func(o *CorsConfig) {
		o.ExposeHeaders = headers
	}
}

// WithAllowCredentials sets whether to allow credentials.
func WithAllowCredentials(allow bool) CorsOption {
	return func(o *CorsConfig) {
		o.AllowCredentials = allow
	}
}

// WithMaxAge sets the max age.
func WithMaxAge(maxAge time.Duration) CorsOption {
	return func(o *CorsConfig) {
		o.MaxAge = maxAge
	}
}

// NewCors creates a new CORS middleware with options.
func NewCors(opts ...CorsOption) gin.HandlerFunc {
	o := defaultCorsConfig()
	o.apply(opts...)
	return cors.New(cors.Config{
		AllowOrigins:     o.AllowOrigins,
		AllowMethods:     o.AllowMethods,
		AllowHeaders:     o.AllowHeaders,
		ExposeHeaders:    o.ExposeHeaders,
		AllowCredentials: o.AllowCredentials,
		MaxAge:           o.MaxAge,
	})
}
