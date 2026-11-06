package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestConfigDocumentServesDefaultRouting(t *testing.T) {
	x, _, _ := configHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodGet, "/api/v1/config/document", "", withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"default_routing":{`)
	assert.Contains(t, rec.Body.String(), `"incident":"sre"`)
}
