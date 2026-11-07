package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHookRejectsHTTPRequests(t *testing.T) {
	assert.True(t, IsHTTPRequest("GET / HTTP/1.1"))
	assert.True(t, IsHTTPRequest("post /x HTTP/1.1"))
	assert.True(t, IsHTTPRequest("OPTIONS * HTTP/1.1"))
	assert.False(t, IsHTTPRequest("trigger 1"))
	assert.False(t, IsHTTPRequest("state"))
	assert.False(t, IsHTTPRequest(""))
}

func TestHookTokenAuth(t *testing.T) {
	assert.True(t, Authorised("state", nil))
	assert.True(t, Authorised("auth s3cret", new("s3cret")))
	assert.False(t, Authorised("auth wrong", new("s3cret")))
	assert.False(t, Authorised("state", new("s3cret")))
	assert.False(t, Authorised("auth ", new("")))
}
