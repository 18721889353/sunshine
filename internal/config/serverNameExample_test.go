package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/18721889353/sunshine/pkg/gofile"

	"github.com/18721889353/sunshine/configs"
)

func TestInit(t *testing.T) {
	configFile := configs.Path("serverNameExample.yml")
	err := Init(configFile)
	if gofile.IsExists(configFile) {
		assert.NoError(t, err)
	} else {
		assert.Error(t, err)
	}

	c := Get()
	assert.NotNil(t, c)

	str := Show()
	assert.NotEmpty(t, str)
	t.Log(str)

	// Set(nil) 应触发 panic
	assert.Panics(t, func() {
		Set(nil)
	})
}

func TestInitNacos(t *testing.T) {
	configFile := configs.Path("serverNameExample_cc.yml")
	_, err := NewCenter(configFile)
	if gofile.IsExists(configFile) {
		assert.NoError(t, err)
	} else {
		assert.Error(t, err)
	}
}
