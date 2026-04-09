package interceptor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClientStatsHandler(t *testing.T) {
	handler := NewClientStatsHandler()
	assert.NotNil(t, handler)
}

func TestNewServerStatsHandler(t *testing.T) {
	handler := NewServerStatsHandler()
	assert.NotNil(t, handler)
}

func TestUnaryClientTracing_Deprecated(t *testing.T) {
	// Deprecated function returns nil in v0.62.0+
	interceptor := UnaryClientTracing()
	assert.Nil(t, interceptor)
}

func TestUnaryServerTracing_Deprecated(t *testing.T) {
	// Deprecated function returns nil in v0.62.0+
	interceptor := UnaryServerTracing()
	assert.Nil(t, interceptor)
}
