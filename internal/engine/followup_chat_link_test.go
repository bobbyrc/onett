package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

func TestQueueFollowupsCarriesChatLink(t *testing.T) {
	cf := newCfRig(t)
	parent := cf.task(domain.SourceChatPrefix+cf.conv, domain.TaskDone)
	plain := cf.task("api", domain.TaskDone)

	res := domain.RunResult{Followups: []domain.FollowupTask{{Title: "more", Kind: domain.KindChore}}}
	cf.e.queueFollowups(context.Background(), parent, res)
	cf.e.queueFollowups(context.Background(), plain, res)

	kids, err := cf.r.st.ListTasks(context.Background(), store.TaskFilter{ParentID: parent.ID})
	require.NoError(t, err)
	require.Len(t, kids, 1)
	assert.Equal(t, "followup", kids[0].Source, "policy matches on Source")
	assert.Equal(t, domain.SourceChatPrefix+cf.conv, kids[0].SourceRef)
	assert.Equal(t, cf.conv, kids[0].ChatConversationID())

	kids, err = cf.r.st.ListTasks(context.Background(), store.TaskFilter{ParentID: plain.ID})
	require.NoError(t, err)
	require.Len(t, kids, 1)
	assert.Empty(t, kids[0].SourceRef, "no chat link to carry")
}

func TestChatFollowupSeesLinkedFollowupTask(t *testing.T) {
	cf := newCfRig(t)
	cf.tick() // seed
	parent := cf.task(domain.SourceChatPrefix+cf.conv, domain.TaskDone)
	cf.e.queueFollowups(context.Background(), parent, domain.RunResult{
		Followups: []domain.FollowupTask{{Title: "more", Kind: domain.KindChore}},
	})
	kids, err := cf.r.st.ListTasks(context.Background(), store.TaskFilter{ParentID: parent.ID})
	require.NoError(t, err)
	require.Len(t, kids, 1)
	cf.tick() // sees it queued
	cf.set(kids[0].ID, domain.TaskDone)
	cf.tick() // sees it settle
	cf.advance(chatFollowupDebounce)
	cf.tick()
	cf.waitIdle()
	require.Equal(t, 1, cf.promptCount())
	assert.Contains(t, cf.prompts[0], kids[0].ID)
}

func TestChatConversationID(t *testing.T) {
	assert.Equal(t, "c1", domain.Task{Source: "chat:c1"}.ChatConversationID())
	assert.Equal(t, "c1", domain.Task{Source: "followup", SourceRef: "chat:c1"}.ChatConversationID())
	assert.Empty(t, domain.Task{Source: "followup", SourceRef: "github_issue:4"}.ChatConversationID())
	assert.Empty(t, domain.Task{Source: "chat:"}.ChatConversationID())
}
