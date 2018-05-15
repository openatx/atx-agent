package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDNSLookup(t *testing.T) {
	ip, err := dnsLookupHost("www.netease.com")
	assert.Nil(t, err)
	t.Logf("www.netease.com -> %s", ip)
}
