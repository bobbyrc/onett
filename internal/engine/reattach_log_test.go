package engine

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// rlBuffer is a log sink the engine's goroutines can write to while the test reads it.
type rlBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *rlBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *rlBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// The daemon log has one INFO line for each run reattached after a restart (#198).
func TestReattachLogsOneDaemonLinePerRun(t *testing.T) {
	const msg = "reattached to a run after restart"
	f := newReattachFixture(t, time.Hour, time.Hour)
	logs := &rlBuffer{}
	f.logOut = logs

	e1, stop1 := f.engine(time.Now)
	task := f.create(e1)
	run := f.waitStoredBothLines(task.ID)
	stop1()
	st := f.shimStatus(run.ID)
	assert.NotContains(t, logs.String(), msg, "the first daemon reattached nothing")

	require.NoError(t, os.WriteFile(f.gate, nil, 0o644))
	rtWait(t, 15*time.Second, "the agent to exit", func() bool { return f.shimStatus(run.ID).Exited })
	_, _ = f.engine(time.Now)
	f.waitTask(task.ID, domain.TaskDone)
	f.waitShimGone(run.ID)

	var lines []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, msg) {
			lines = append(lines, line)
		}
	}
	require.Len(t, lines, 1, "one line per reattached run")
	assert.Contains(t, lines[0], "level=INFO")
	assert.Contains(t, lines[0], "run="+run.ID)
	assert.Contains(t, lines[0], "task="+task.ID)
	assert.Contains(t, lines[0], fmt.Sprintf("shim_pid=%d", st.ShimPID))
}
