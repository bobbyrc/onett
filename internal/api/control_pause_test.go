package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestPauseInfoRoundTripsThroughControlAndStats(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodPost, "/api/v1/control/pause", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var c ControlState
	decode(t, rec, &c)
	require.NotNil(t, c.Pause)
	assert.Equal(t, domain.PauseOwner, c.Pause.Reason)
	assert.Equal(t, "api token", c.Pause.By)

	rec = x.call(t, http.MethodGet, "/api/v1/stats", "")
	var s Stats
	decode(t, rec, &s)
	require.NotNil(t, s.Pause)
	assert.Equal(t, domain.PauseOwner, s.Pause.Reason)

	rec = x.call(t, http.MethodPost, "/api/v1/control/resume", "")
	decode(t, rec, &c)
	assert.False(t, c.Paused)
	assert.Nil(t, c.Pause)
}
