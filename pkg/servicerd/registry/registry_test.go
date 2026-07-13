package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServiceInstance(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		s := NewServiceInstance("foo", "bar", []string{"grpc://127.0.0.1:8282"},
			WithVersion("v1.0.0"),
			WithMetadata(map[string]string{"foo": "bar"}),
		)
		assert.NotNil(t, s)
		assert.Equal(t, "foo", s.ID)
		assert.Equal(t, "bar", s.Name)
		assert.Equal(t, "v1.0.0", s.Version)
		assert.Equal(t, map[string]string{"foo": "bar"}, s.Metadata)
		assert.Equal(t, []string{"grpc://127.0.0.1:8282"}, s.Endpoints)
	})

	t.Run("no options", func(t *testing.T) {
		s := NewServiceInstance("id1", "svc1", []string{"http://127.0.0.1:8080"})
		assert.NotNil(t, s)
		assert.Equal(t, "id1", s.ID)
		assert.Equal(t, "svc1", s.Name)
		assert.Empty(t, s.Version)
		assert.Nil(t, s.Metadata)
	})

	t.Run("nil endpoints", func(t *testing.T) {
		s := NewServiceInstance("id2", "svc2", nil)
		assert.NotNil(t, s)
		assert.Nil(t, s.Endpoints)
	})

	t.Run("empty endpoints", func(t *testing.T) {
		s := NewServiceInstance("id3", "svc3", []string{})
		assert.NotNil(t, s)
		assert.Empty(t, s.Endpoints)
	})

	t.Run("multiple endpoints", func(t *testing.T) {
		s := NewServiceInstance("id4", "svc4", []string{"grpc://127.0.0.1:8282", "http://127.0.0.1:8080"})
		assert.NotNil(t, s)
		assert.Len(t, s.Endpoints, 2)
	})

	t.Run("only version", func(t *testing.T) {
		s := NewServiceInstance("id5", "svc5", []string{"grpc://127.0.0.1:8282"}, WithVersion("v2.0.0"))
		assert.Equal(t, "v2.0.0", s.Version)
		assert.Nil(t, s.Metadata)
	})

	t.Run("only metadata", func(t *testing.T) {
		s := NewServiceInstance("id6", "svc6", []string{"grpc://127.0.0.1:8282"}, WithMetadata(map[string]string{"key": "val"}))
		assert.Empty(t, s.Version)
		assert.Equal(t, map[string]string{"key": "val"}, s.Metadata)
	})

	t.Run("nil metadata value", func(t *testing.T) {
		s := NewServiceInstance("id7", "svc7", []string{"grpc://127.0.0.1:8282"}, WithMetadata(nil))
		assert.Nil(t, s.Metadata)
	})

	t.Run("empty id", func(t *testing.T) {
		s := NewServiceInstance("", "svc8", []string{"grpc://127.0.0.1:8282"})
		assert.Empty(t, s.ID)
		assert.Equal(t, "svc8", s.Name)
	})

	t.Run("empty name", func(t *testing.T) {
		s := NewServiceInstance("id9", "", []string{"grpc://127.0.0.1:8282"})
		assert.Empty(t, s.Name)
	})
}

func TestWithVersion(t *testing.T) {
	t.Run("empty version", func(t *testing.T) {
		s := NewServiceInstance("id", "svc", []string{"grpc://127.0.0.1:8282"}, WithVersion(""))
		assert.Empty(t, s.Version)
	})

	t.Run("semver version", func(t *testing.T) {
		s := NewServiceInstance("id", "svc", []string{"grpc://127.0.0.1:8282"}, WithVersion("1.0.0-beta.1"))
		assert.Equal(t, "1.0.0-beta.1", s.Version)
	})
}

func TestWithMetadata(t *testing.T) {
	t.Run("multiple pairs", func(t *testing.T) {
		s := NewServiceInstance("id", "svc", []string{"grpc://127.0.0.1:8282"},
			WithMetadata(map[string]string{"env": "prod", "region": "us-east-1", "zone": "a"}))
		assert.Len(t, s.Metadata, 3)
		assert.Equal(t, "prod", s.Metadata["env"])
		assert.Equal(t, "us-east-1", s.Metadata["region"])
	})
}

func TestOptionsInterface(t *testing.T) {
	t.Run("Option type assertion", func(t *testing.T) {
		var opt Option = WithVersion("v1")
		assert.NotNil(t, opt)

		var opt2 Option = WithMetadata(map[string]string{"k": "v"})
		assert.NotNil(t, opt2)
	})
}
