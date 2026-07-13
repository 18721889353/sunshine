package server

import (
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// CronOption setting up cron
type CronOption func(*cronOptions)

type cronOptions struct {
	instance  *registry.ServiceInstance
	iRegistry registry.Registry
}

func defaultCronOptions() *cronOptions {
	return &cronOptions{
		instance:  nil,
		iRegistry: nil,
	}
}

func (o *cronOptions) apply(opts ...CronOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithCronRegistry registration services
func WithCronRegistry(iRegistry registry.Registry, instance *registry.ServiceInstance) CronOption {
	return func(o *cronOptions) {
		o.iRegistry = iRegistry
		o.instance = instance
	}
}
