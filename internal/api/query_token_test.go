package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/auth"
)

// refusedEvents sends a GET /events that should be refused. The timeout keeps a stream that is
// wrongly accepted from hanging the test.
func refusedEvents(t *testing.T, x *harness, target string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req := testRequest(http.MethodGet, target, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	x.h.ServeHTTP(rec, req)
	return rec
}

func TestEventsRefusesTheTokenInTheURL(t *testing.T) {
	x := newHarness(t)
	assert.Equal(t, http.StatusUnauthorized, refusedEvents(t, x, "/api/v1/events?token="+testToken).Code)
	assert.Equal(t, http.StatusUnauthorized, refusedEvents(t, x, "/api/v1/events?token="+auth.AgentToken(testToken)).Code)

	resp := openStream(t, x, "/api/v1/events", testToken)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
