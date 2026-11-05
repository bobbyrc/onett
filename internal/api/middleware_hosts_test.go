package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

// AllowedHostsFunc is read on every request, so a reload changes the guard with no restart.
func TestHostGuardFollowsAllowedHostsFunc(t *testing.T) {
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	var hosts atomic.Value
	hosts.Store([]string{})
	h := NewServer(Options{
		Store: st, Bus: events.NewBus(st, nil), Controller: &fakeCtl{cfg: testConfig()}, Token: testToken,
		AllowedHostsFunc: func() []string { return hosts.Load().([]string) },
	})
	status := func(host string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusMisdirectedRequest, status("onett.lan"))
	hosts.Store([]string{"onett.lan"})
	assert.Equal(t, http.StatusOK, status("onett.lan"))
	hosts.Store([]string{})
	assert.Equal(t, http.StatusMisdirectedRequest, status("onett.lan"))
	assert.Equal(t, http.StatusOK, status("localhost"))
}
