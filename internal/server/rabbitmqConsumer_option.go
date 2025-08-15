package server

import (
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// RABBITQMCONSUMEROption setting up rabbitmqConsumer
type RABBITQMCONSUMEROption func(*rabbitmqConsumerOptions)

type rabbitmqConsumerOptions struct {
	isProd    bool
	instance  *registry.ServiceInstance
	iRegistry registry.Registry
}

func defaultRABBITQMCONSUMEROptions() *rabbitmqConsumerOptions {
	return &rabbitmqConsumerOptions{
		isProd:    false,
		instance:  nil,
		iRegistry: nil,
	}
}

func (o *rabbitmqConsumerOptions) apply(opts ...RABBITQMCONSUMEROption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithRABBITQMCONSUMERRegistry registration services
func WithRABBITQMCONSUMERRegistry(iRegistry registry.Registry, instance *registry.ServiceInstance) RABBITQMCONSUMEROption {
	return func(o *rabbitmqConsumerOptions) {
		o.iRegistry = iRegistry
		o.instance = instance
	}
}
