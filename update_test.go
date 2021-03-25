package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatString(t *testing.T) {
	s := formatString("a {v} {b} {v}", map[string]string{
		"v": "x",
		"b": "y",
	})
	assert.Equal(t, s, "a x y x")
}

func TestGetLatestVersion(t *testing.T) {
	version, err := getLatestVersion()
	assert.NoError(t, err)
	t.Logf("version: %s", version)
	assert.NotEqual(t, version, "")
}
