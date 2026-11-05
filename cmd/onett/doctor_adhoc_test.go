package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDoctorWarnsOnAdhocSignedBinaryWithVolumePath(t *testing.T) {
	cases := []struct {
		name string
		os   string
		out  string
		warn bool
	}{
		{"adhoc on darwin", "darwin", "Identifier=a.out\nSignature=adhoc\n", true},
		{"certificate signed", "darwin", "Identifier=io.onett.onett\nAuthority=My Cert\n", false},
		{"not darwin", "linux", "Signature=adhoc\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old := goos
			goos = c.os
			t.Cleanup(func() { goos = old })
			fakeRunner(t, func(name string, args ...string) (string, error) { return c.out, nil })
			var buf bytes.Buffer
			d := &doctor{out: &buf}
			d.checkAdhocSigning()
			if c.warn {
				assertLine(t, buf.String(), "warn", "ad-hoc signed")
				assert.Contains(t, buf.String(), "ONETT_CODESIGN_IDENTITY")
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}

func TestUnderVolumes(t *testing.T) {
	assert.True(t, underVolumes("/Volumes/SSK/tools"))
	assert.False(t, underVolumes(t.TempDir()))
}
