package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestPauseReasonRoundTripsThroughControlAndStats(t *testing.T) {
	x := newHarness(t)
	_, err := x.st.CreateRun(t.Context(), domain.Run{TaskID: "task-1", Project: "onett", Status: domain.RunRunning, StartedAt: time.Now()})
	require.NoError(t, err)
	_, err = x.st.CreateRun(t.Context(), domain.Run{Project: "onett", Role: "orchestrator", Status: domain.RunRunning, StartedAt: time.Now()})
	require.NoError(t, err)

	rec := x.call(t, http.MethodPost, "/api/v1/control/pause", `{"reason":"self-update","detail":"69f1660"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var c ControlState
	decode(t, rec, &c)
	require.NotNil(t, c.Pause)
	assert.Equal(t, domain.PauseSelfUpdate, c.Pause.Reason)
	assert.Equal(t, "69f1660", c.Pause.Detail)
	assert.Equal(t, "api token", c.Pause.By)
	assert.Equal(t, []PauseWait{{RunID: c.Pause.Waiting[0].RunID, TaskID: "task-1"}}, c.Pause.Waiting, "chat turns are not waited on")

	rec = x.call(t, http.MethodGet, "/api/v1/stats", "")
	var s Stats
	decode(t, rec, &s)
	require.NotNil(t, s.Pause)
	assert.Equal(t, "69f1660", s.Pause.Detail)

	rec = x.call(t, http.MethodPost, "/api/v1/control/resume", "")
	decode(t, rec, &c)
	assert.False(t, c.Paused)
	assert.Nil(t, c.Pause)
}

func TestPauseWithoutBodyIsOwner(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodPost, "/api/v1/control/pause", "")
	var c ControlState
	decode(t, rec, &c)
	require.NotNil(t, c.Pause)
	assert.Equal(t, domain.PauseOwner, c.Pause.Reason)
}

func TestPauseRejectsUnknownFieldAndMultilineDetail(t *testing.T) {
	x := newHarness(t)
	for _, body := range []string{`{"why":"x"}`, `{"detail":"a\nb"}`} {
		rec := x.call(t, http.MethodPost, "/api/v1/control/pause", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}
