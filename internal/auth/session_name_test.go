package auth

import "testing"

func TestSessionCookieNameUsesThePort(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:7777": "onett_session_7777",
		":7777":          "onett_session_7777",
		"[::1]:8080":     "onett_session_8080",
		"":               "onett_session",
		"127.0.0.1:0":    "onett_session",
		"localhost":      "onett_session",
		"127.0.0.1:http": "onett_session",
	}
	for addr, want := range cases {
		if got := SessionCookieName(addr); got != want {
			t.Errorf("SessionCookieName(%q) = %q, want %q", addr, got, want)
		}
	}
}
