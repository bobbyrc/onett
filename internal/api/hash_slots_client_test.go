package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slotRequest is a sign-in request from addr whose wait for a slot ends after timeout.
func slotRequest(addr string, timeout time.Duration) (*http.Request, context.CancelFunc) {
	req := testRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = addr
	ctx, cancel := context.WithTimeout(req.Context(), timeout)
	return req.WithContext(ctx), cancel
}

// takeFor takes a hashing slot for a request from addr. A slot taken is given back at the end of
// the test; release may also be called earlier, and calling it twice is harmless.
func takeFor(t *testing.T, addr string, timeout time.Duration) (func(), bool, *httptest.ResponseRecorder) {
	t.Helper()
	req, cancel := slotRequest(addr, timeout)
	defer cancel()
	rec := httptest.NewRecorder()
	release, ok := takeHashSlot(rec, req)
	if !ok {
		return func() {}, false, rec
	}
	var once sync.Once
	rel := func() { once.Do(release) }
	t.Cleanup(rel)
	return rel, true, rec
}

func TestOneAddressCannotHoldEveryHashingSlot(t *testing.T) {
	var held []func()
	for i := 0; i < hashSlotsPerClient; i++ {
		rel, ok, _ := takeFor(t, "203.0.113.7:50000", 10*time.Second)
		require.True(t, ok, "slot %d of this address", i+1)
		held = append(held, rel)
	}

	rel, ok, rec := takeFor(t, "203.0.113.7:50001", 300*time.Millisecond)
	assert.False(t, ok, "a third slot for the same address must wait and give up")
	assert.Equal(t, http.StatusOK, rec.Code, "a request whose context ended writes nothing")
	assert.Empty(t, rec.Body.String())
	rel()

	other, ok, _ := takeFor(t, "198.51.100.9:60000", 10*time.Second)
	assert.True(t, ok, "another address gets a slot")
	held = append(held, other)

	for _, r := range held {
		r()
	}
	hashLines.mu.Lock()
	_, busy1 := hashLines.lines["203.0.113.7"]
	_, busy2 := hashLines.lines["198.51.100.9"]
	hashLines.mu.Unlock()
	assert.False(t, busy1, "an unused line is dropped")
	assert.False(t, busy2, "an unused line is dropped")
}

func TestLoopbackIsNotLimitedPerClient(t *testing.T) {
	for i := 0; i < hashSlotsPerClient+1; i++ {
		_, ok, _ := takeFor(t, "127.0.0.1:40000", 10*time.Second)
		assert.True(t, ok, "loopback slot %d", i+1)
	}
}

func TestIPv6ClientsShareALinePerNetwork(t *testing.T) {
	_, ok, _ := takeFor(t, "[2001:db8:1:2::a]:1000", 10*time.Second)
	require.True(t, ok)
	_, ok, _ = takeFor(t, "[2001:db8:1:2::b]:1000", 10*time.Second)
	require.True(t, ok)

	_, ok, _ = takeFor(t, "[2001:db8:1:2::c]:1000", 300*time.Millisecond)
	assert.False(t, ok, "a third address in the same /64 shares the line")

	_, ok, _ = takeFor(t, "[2001:db8:9:9::a]:1000", 10*time.Second)
	assert.True(t, ok, "an address in another /64 has its own line")
}

func TestClientKey(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7:1":                 "203.0.113.7",
		"127.0.0.1:1":                   "",
		"[::1]:1":                       "",
		"[::ffff:127.0.0.1]:1":          "",
		"[::ffff:203.0.113.7]:1":        "203.0.113.7",
		"[2001:db8:1:2::a]:1":           "2001:db8:1:2::/64",
		"[fd7a:115c:a1e0::53:a01f]:1":   "fd7a:115c:a1e0::53:a01f",
		"[fd7a:115c:a1e0::1234:5678]:1": "fd7a:115c:a1e0::1234:5678",
		"[fe80::1%en0]:1":               "fe80::1",
		"garbage":                       "",
	}
	for addr, want := range cases {
		req := testRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = addr
		assert.Equal(t, want, clientKey(req), "RemoteAddr %q", addr)
	}
}
