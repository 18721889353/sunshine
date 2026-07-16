package server

import (
	"time"

	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// HTTPOption setting up http
type HTTPOption func(*httpOptions)

type httpOptions struct {
	isProd             bool
	instance           *registry.ServiceInstance
	iRegistry          registry.Registry
	readTimeout        time.Duration // HTTP 读取超时（0=不限制）
	writeTimeout       time.Duration // HTTP 写入超时（0=不限制）
	readHeaderTimeout  time.Duration // HTTP 请求头读取超时（0=不限制）
	idleTimeout        time.Duration // HTTP keep-alive 空闲超时（0=不限制）
}

func defaultHTTPOptions() *httpOptions {
	return &httpOptions{
		isProd:    false,
		instance:  nil,
		iRegistry: nil,
	}
}

func (o *httpOptions) apply(opts ...HTTPOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithHTTPIsProd setting up production environment markers
func WithHTTPIsProd(isProd bool) HTTPOption {
	return func(o *httpOptions) {
		o.isProd = isProd
	}
}

// WithHTTPReadTimeout 设置 HTTP 读取超时
func WithHTTPReadTimeout(timeout time.Duration) HTTPOption {
	return func(o *httpOptions) {
		o.readTimeout = timeout
	}
}

// WithHTTPWriteTimeout 设置 HTTP 写入超时
func WithHTTPWriteTimeout(timeout time.Duration) HTTPOption {
	return func(o *httpOptions) {
		o.writeTimeout = timeout
	}
}

// WithHTTPReadHeaderTimeout 设置 HTTP 请求头读取超时
func WithHTTPReadHeaderTimeout(timeout time.Duration) HTTPOption {
	return func(o *httpOptions) {
		o.readHeaderTimeout = timeout
	}
}

// WithHTTPIdleTimeout 设置 HTTP keep-alive 空闲超时
func WithHTTPIdleTimeout(timeout time.Duration) HTTPOption {
	return func(o *httpOptions) {
		o.idleTimeout = timeout
	}
}

// WithHTTPRegistry registration services
func WithHTTPRegistry(iRegistry registry.Registry, instance *registry.ServiceInstance) HTTPOption {
	return func(o *httpOptions) {
		o.iRegistry = iRegistry
		o.instance = instance
	}
}
