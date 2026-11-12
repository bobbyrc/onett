package api

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

// newHarnessWith is newHarness with the server options changed by mod.
func newHarnessWith(t *testing.T, mod func(*Options)) *harness {
	t.Helper()
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewBus(st, logger)
	ctl := &fakeCtl{cfg: testConfig(), errs: map[string]error{}}
	x := &harness{st: st, bus: bus, ctl: ctl}
	o := Options{
		Store:      st,
		Bus:        bus,
		Controller: ctl,
		Token:      testToken,
		Version:    "test-version",
		Web:        fstest.MapFS{},
		Restart:    func() { x.restarts++ },
		Logger:     logger,
	}
	mod(&o)
	x.h = NewServer(o)
	return x
}

// signIn logs alice in through the sign-in route and returns the one cookie it sets.
func signIn(t *testing.T, x *harness) *http.Cookie {
	t.Helper()
	rec := x.authReq(t, http.MethodPost, "/api/v1/auth/login", `{"username":"alice","password":"`+testPassword+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	return cookies[0]
}

func TestAnotherInstanceDoesNotClearTheSessionCookie(t *testing.T) {
	a := newHarnessWith(t, func(o *Options) { o.ListenAddr = "127.0.0.1:7777" })
	b := newHarnessWith(t, func(o *Options) { o.ListenAddr = "127.0.0.1:7797" })
	a.newUserSession(t, "alice", domain.RoleOwner)
	c := signIn(t, a)
	assert.Equal(t, "onett_session_7777", c.Name)

	// The same cookie, sent to the other instance on the same host name.
	send := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value}) }
	rec := b.authReq(t, http.MethodGet, "/api/v1/stats", "", send)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, rec.Header().Values("Set-Cookie"), "B must not clear A's cookie")

	rec = a.authReq(t, http.MethodGet, "/api/v1/stats", "", send)
	assert.Equal(t, http.StatusOK, rec.Code)
}
