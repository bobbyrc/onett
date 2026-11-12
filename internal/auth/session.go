package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"time"
)

// SessionCookie is the base of the browser cookie name that carries a session token. See
// SessionCookieName for the full name.
const SessionCookie = "onett_session"

// SessionCookieName returns the name of the session cookie for the onett that listens on
// listenAddr (host:port): SessionCookie, an underscore and the port, such as
// "onett_session_7777". Browsers keep one cookie jar per host name and ignore the port, so two
// onetts on one machine (the real one and a staging copy) would otherwise overwrite and clear
// each other's session. An address with no numeric port, or port 0, gives SessionCookie alone.
func SessionCookieName(listenAddr string) string {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" || port == "0" {
		return SessionCookie
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return SessionCookie
		}
	}
	return SessionCookie + "_" + port
}

// SessionTTL is how long a session lasts after sign-in.
const SessionTTL = 30 * 24 * time.Hour

// NewSessionToken makes a new random session. value goes only in the browser's cookie; id is
// what the server stores. Keeping only a hash of the cookie means a leaked database cannot be
// replayed as signed-in cookies.
func NewSessionToken() (value, id string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("read session token: %w", err)
	}
	value = base64.RawURLEncoding.EncodeToString(b)
	return value, SessionID(value), nil
}

// SessionID returns the stored identifier for a cookie value: the lowercase hex SHA-256 of it.
func SessionID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
