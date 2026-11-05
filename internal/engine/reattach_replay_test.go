package engine

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// Output the agent wrote while no daemon was connected, and that its shim then lost, is still in
// stream.log: recover stores it on the interrupted run.
func TestRestartWithoutShimStoresOutputPastTheOffset(t *testing.T) {
	f := newReattachFixture(t, time.Hour, time.Hour)
	e1, stop1 := f.engine(time.Now)
	task := f.create(e1)
	run := f.waitStoredBothLines(task.ID)
	stop1()

	// The agent writes lines three and four, then the shim dies before anyone reads them.
	require.NoError(t, os.WriteFile(f.gate, nil, 0o644))
	rtWait(t, 15*time.Second, "the agent to exit", func() bool { return f.shimStatus(run.ID).Exited })
	st := f.shimStatus(run.ID)
	require.NoError(t, syscall.Kill(st.ShimPID, syscall.SIGKILL))
	rtWait(t, 15*time.Second, "shim to die", func() bool { return syscall.Kill(st.ShimPID, 0) == syscall.ESRCH })

	_, _ = f.engine(time.Now)
	f.waitTask(task.ID, domain.TaskDone)
	runs, err := f.st.ListRuns(context.Background(), store.RunFilter{TaskID: task.ID})
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, domain.RunInterrupted, runs[1].Status)
	assert.Equal(t, []string{"line one", "line two", "line three", "line four"}, f.logTexts(runs[1].ID),
		"the interrupted run keeps the output written after its last stored offset, once")
	// The closing result line produces no event, so the offset may stop short of the file's end.
	assert.Greater(t, runs[1].StreamOffset, run.StreamOffset)
	assert.LessOrEqual(t, runs[1].StreamOffset, f.streamSize(runs[1].ID))
}
