package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestOwnerCannotSetTheirOwnPasswordThroughTheUsersRoute(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	const want = `{"error":"change your own password with POST /auth/password, which asks for the current one"}`

	for _, name := range []string{"boss", "BOSS"} {
		rec := x.authReq(t, http.MethodPost, "/api/v1/users/"+name+"/password",
			`{"password":"a brand new passphrase"}`, withCookie(c))
		require.Equal(t, http.StatusForbidden, rec.Code, name+": "+rec.Body.String())
		assert.JSONEq(t, want, rec.Body.String())
	}

	rec := x.authReq(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"boss","password":"`+testPassword+`"}`)
	assert.Equal(t, http.StatusOK, rec.Code, "the refused reset must leave the password unchanged")

	rec = x.authReq(t, http.MethodGet, "/api/v1/users", "", withCookie(c))
	assert.Equal(t, http.StatusOK, rec.Code, "the refused reset must keep the session")
}

func TestOwnerStillSetsAnotherUsersPassword(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	v := x.newUserSession(t, "vic", domain.RoleViewer)

	rec := x.authReq(t, http.MethodPost, "/api/v1/users/vic/password",
		`{"password":"a brand new passphrase"}`, withCookie(c))
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rec = x.authReq(t, http.MethodGet, "/api/v1/stats", "", withCookie(v))
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "vic's old session must end")

	rec = x.authReq(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"vic","password":"a brand new passphrase"}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
