package server

import (
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// CRONOption setting up cron
type CRONOption func(*cronOptions)

type cronOptions struct {
	isProd    bool
	instance  *registry.ServiceInstance
	iRegistry registry.Registry
}

func defaultCRONOptions() *cronOptions {
	return &cronOptions{
		isProd:    false,
		instance:  nil,
		iRegistry: nil,
	}
}

func (o *cronOptions) apply(opts ...CRONOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithCRONRegistry registration services
func WithCRONRegistry(iRegistry registry.Registry, instance *registry.ServiceInstance) CRONOption {
	return func(o *cronOptions) {
		o.iRegistry = iRegistry
		o.instance = instance
	}
}
