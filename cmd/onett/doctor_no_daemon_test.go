package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDoctorNoDaemonSkipsDaemonChecks(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "9.9.9"})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ONETT_API", srv.URL)

	f := newDoctorFixture(t, "")
	toolRunner(t, nil)

	code, out, errOut := runCLI("doctor", "--config", f.cfg, "--no-daemon")
	assert.Equal(t, 0, code, out+errOut)
	assert.Equal(t, int32(0), hits.Load(), "--no-daemon must not contact the daemon")
	assertLine(t, out, "skip", "daemon: not checked (--no-daemon)")
	assert.NotContains(t, out, "daemon: running")
}

func TestDoctorNoDaemonInHelp(t *testing.T) {
	code, out, errOut := runCLI("doctor", "-h")
	assert.Equal(t, 0, code, errOut)
	assert.True(t, strings.Contains(out+errOut, "-no-daemon"), out+errOut)
}
