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
