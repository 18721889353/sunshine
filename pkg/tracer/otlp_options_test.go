package tracer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsHTTPEndpoint(t *testing.T) {
	cases := map[string]bool{
		"localhost:4317":                       false,
		"http://localhost:4318":                false, // 无 path
		"http://host:4318/v1/traces":           true,
		"https://tracing.aliyuncs.com/x/api/y": true,
		"http:///foo":                          false, // host 为空
		"http://host:4318/":                    false, // path 仅 "/"
		"":                                     false,
		"grpc://host:4317":                     false, // 非 http/https scheme
	}
	for input, want := range cases {
		assert.Equal(t, want, isHTTPEndpoint(input), "endpoint=%s", input)
	}
}

func TestDefaultOTLPOptions(t *testing.T) {
	o := defaultOTLPOptions()
	assert.Equal(t, "localhost:4317", o.endpoint)
	assert.True(t, o.insecure)
	assert.Equal(t, 10*time.Second, o.timeout)
}

func TestOTLPOptionApply(t *testing.T) {
	o := defaultOTLPOptions()

	WithEndpoint("custom:4317")(o)
	assert.Equal(t, "custom:4317", o.endpoint)

	WithInsecure(false)(o)
	assert.False(t, o.insecure)

	WithHeaders(map[string]string{"key": "val"})(o)
	assert.Equal(t, map[string]string{"key": "val"}, o.headers)

	WithTimeout(5 * time.Second)(o)
	assert.Equal(t, 5*time.Second, o.timeout)
}
