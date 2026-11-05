package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/workspace"
)

func TestTaskAttachmentNote(t *testing.T) {
	assert.Empty(t, taskAttachmentNote(nil))
	note := taskAttachmentNote([]workspace.Placed{
		{Attachment: domain.Attachment{Name: "shot.png", MIME: "image/png", Size: 42},
			Rel: ".onett-run/attachments/shot.png", Abs: "/wt/.onett-run/attachments/shot.png"},
		{Attachment: domain.Attachment{Name: "old.log", MIME: "text/plain", Size: 7, Expired: true}},
	})
	assert.Contains(t, note, "shot.png (image/png, 42 bytes): `.onett-run/attachments/shot.png` (/wt/.onett-run/attachments/shot.png)")
	assert.Contains(t, note, "Read tool")
	assert.Contains(t, note, "old.log (text/plain, 7 bytes): expired")
}

func TestOrchestratorNoteShowsIDAndAttachFlag(t *testing.T) {
	note := attachmentNote([]domain.Attachment{{ID: "att-9", Name: "n.txt", MIME: "text/plain", Size: 5, Path: "/d/n.txt"}})
	assert.Contains(t, note, "ID att-9")
	assert.Contains(t, note, "onett task create --attach <id>")
	assert.NotContains(t, note, "copy what it needs")
}
