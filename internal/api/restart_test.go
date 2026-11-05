package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestRestartAsOwnerAnswersThenRestarts(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPost, "/api/v1/restart", "", withCookie(c))
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, 1, x.restarts)
}

func TestRestartRefusesTheAPIToken(t *testing.T) {
	x := newHarness(t)

	rec := x.do(t, http.MethodPost, "/api/v1/restart", "", testToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Zero(t, x.restarts)
}

func TestRestartRefusesAnOperator(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "op", domain.RoleOperator)

	rec := x.authReq(t, http.MethodPost, "/api/v1/restart", "", withCookie(c))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Zero(t, x.restarts)
}
