package server

import (
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

// RabbitmqConsumerOption setting up rabbitmqConsumer
type RabbitmqConsumerOption func(*rabbitmqConsumerOptions)

type rabbitmqConsumerOptions struct {
	instance  *registry.ServiceInstance
	iRegistry registry.Registry
}

func defaultRabbitmqConsumerOptions() *rabbitmqConsumerOptions {
	return &rabbitmqConsumerOptions{
		instance:  nil,
		iRegistry: nil,
	}
}

func (o *rabbitmqConsumerOptions) apply(opts ...RabbitmqConsumerOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithRabbitmqConsumerRegistry registration services
func WithRabbitmqConsumerRegistry(iRegistry registry.Registry, instance *registry.ServiceInstance) RabbitmqConsumerOption {
	return func(o *rabbitmqConsumerOptions) {
		o.iRegistry = iRegistry
		o.instance = instance
	}
}
