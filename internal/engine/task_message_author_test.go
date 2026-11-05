package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestTaskMessageStoresAuthor(t *testing.T) {
	env, _, _, _ := fuEnv(t)
	require.NoError(t, env.e.SetPaused(env.ctx, true))
	task := env.create(domain.Task{Body: "make a thing"})

	_, err := env.e.SendTaskMessage(env.ctx, task.ID, "from the owner", "")
	require.NoError(t, err)
	_, err = env.e.SendTaskMessage(env.ctx, task.ID, "from the orchestrator", domain.AuthorOrchestrator)
	require.NoError(t, err)

	msgs := env.messages(task.ID)
	require.Len(t, msgs, 2)
	assert.Empty(t, msgs[0].Author)
	assert.Equal(t, domain.AuthorOrchestrator, msgs[1].Author)
}
