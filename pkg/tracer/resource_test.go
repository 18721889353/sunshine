package tracer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewResource(t *testing.T) {
	resource := NewResource()
	assert.NotNil(t, resource)
}

func TestWithAttributes(t *testing.T) {
	testData := map[string]string{}
	o := new(resourceConfig)
	applyResourceOptions(o, WithAttributes(testData))
	assert.Equal(t, testData, o.attributes)
}

func TestWithEnvironment(t *testing.T) {
	testData := "env"
	o := new(resourceConfig)
	applyResourceOptions(o, WithEnvironment(testData))
	assert.Equal(t, testData, o.environment)
}

func TestWithServiceName(t *testing.T) {
	testData := "foo"
	o := new(resourceConfig)
	applyResourceOptions(o, WithServiceName(testData))
	assert.Equal(t, testData, o.serviceName)
}

func TestWithServiceVersion(t *testing.T) {
	testData := "v1.0"
	o := new(resourceConfig)
	applyResourceOptions(o, WithServiceVersion(testData))
	assert.Equal(t, testData, o.serviceVersion)
}

func TestApplyResourceOptions(t *testing.T) {
	testData := "v1.0"
	o := new(resourceConfig)
	applyResourceOptions(o, WithServiceVersion(testData))
	assert.Equal(t, testData, o.serviceVersion)
}
