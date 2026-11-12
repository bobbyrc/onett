package api

import (
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/domain"
)

// restartAddrHarness is configHarness with the server told which address it is bound to, and the
// config file naming newAddr instead.
func restartAddrHarness(t *testing.T, newAddr string) *harness {
	t.Helper()
	x, path, text := configHarness(t)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(text, "127.0.0.1:7777", newAddr, 1)), 0o600))
	x.h = NewServer(Options{
		Store: x.st, Bus: x.bus, Controller: x.ctl, Token: testToken, Version: "test-version",
		Web: fstest.MapFS{}, ListenAddr: "127.0.0.1:7777", Restart: func() { x.restarts++ },
	})
	return x
}

// restartCookie is withCookie for restartAddrHarness, whose server names its session cookie
// after the port it listens on.
func restartCookie(value string) reqOpt {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: auth.SessionCookieName("127.0.0.1:7777"), Value: value})
	}
}

func TestRestartRefusesAnAddrThatCannotBeBound(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	x := restartAddrHarness(t, l.Addr().String())
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPost, "/api/v1/restart", "", restartCookie(c))
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "cannot be bound")
	assert.Zero(t, x.restarts)
}

func TestRestartAllowsAFreeAddr(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	x := restartAddrHarness(t, addr)
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPost, "/api/v1/restart", "", restartCookie(c))
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, 1, x.restarts)
}

func TestRestartSkipsTheProbeForTheRunningAddr(t *testing.T) {
	x := restartAddrHarness(t, "127.0.0.1:7777")
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPost, "/api/v1/restart", "", restartCookie(c))
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}
