package app

import "strings"

// TestHookTokenEnv optionally requires `auth <token>` as the first line of each connection.
const TestHookTokenEnv = "RKL_TEST_HOOK_TOKEN"

// IsHTTPRequest reports a line that starts an HTTP request: the hook speaks a line protocol on
// 127.0.0.1, and a browser page must not be able to drive it with a fetch.
func IsHTTPRequest(line string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(line), " ")
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH", "CONNECT", "TRACE"} {
		if strings.EqualFold(first, m) {
			return true
		}
	}
	return false
}

// Authorised reports whether line unlocks a connection when a token is required (nil token:
// no authentication).
func Authorised(line string, token *string) bool {
	if token == nil {
		return true
	}
	got, ok := strings.CutPrefix(line, "auth ")
	return ok && *token != "" && strings.TrimSpace(got) == *token
}
