package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// setVersion replaces this binary's version for one test.
func setVersion(t *testing.T, v string) {
	t.Helper()
	prev := version
	version = v
	t.Cleanup(func() { version = prev })
}

func TestDoctorVersionWarnsWhenDaemonDiffers(t *testing.T) {
	setVersion(t, "1.0.0")
	healthyDaemon(t) // reports 9.9.9
	f := newDoctorFixture(t, "")
	toolRunner(t, nil)

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 0, code, out)
	assertLine(t, out, "warn", "daemon: runs version 9.9.9, but this onett is version 1.0.0")
	assertLine(t, out, "warn", "restart the daemon")
	assertLine(t, out, "ok", "daemon: running at")
}

func TestDoctorVersionQuietWhenDaemonMatches(t *testing.T) {
	setVersion(t, "9.9.9")
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	toolRunner(t, nil)

	_, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.NotContains(t, out, "but this onett is version")
}
