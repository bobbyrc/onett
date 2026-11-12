package github

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/scm"
)

// A job can fail while another job of the same run, or another run of the same commit, is still
// going. gh prints no log until the run ends, so CILog must say "not yet" and fetch nothing.
func TestCILogReportsRunningWhileARunOfTheHeadIsUnfinished(t *testing.T) {
	f := newFake()
	f.on["pr view"] = `{"number":2,"state":"OPEN","headRefName":"onett/x","headRefOid":"0123456789abcdef","statusCheckRollup":[]}`
	f.on["run list"] = `[{"databaseId":101,"conclusion":"failure","name":"lint"},{"databaseId":102,"conclusion":"","name":"CI"}]`
	f.on["run view"] = "must not be fetched"
	g := NewWithRunner(repo, f.runner())

	log, err := g.CILog(context.Background(), 2, 10000)
	require.ErrorIs(t, err, scm.ErrCIRunning)
	assert.Empty(t, log)
	assert.Zero(t, f.countCalls("run", "view"))
}
