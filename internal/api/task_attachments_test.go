package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTaskWithAttachments(t *testing.T) {
	e := newAttachEnv(t, 1<<20) // max_per_message is 2
	a := e.uploadOK(t, "a.png", pngBytes)
	b := e.uploadOK(t, "b.png", pngBytes)
	c := e.uploadOK(t, "c.png", pngBytes)
	post := func(atts string) int {
		rec := e.x.authReq(t, http.MethodPost, "/api/v1/tasks",
			`{"project":"demo","title":"t","attachments":[`+atts+`]}`, withToken(testToken))
		return rec.Code
	}

	require.Equal(t, http.StatusCreated, post(`"`+a.ID+`","`+b.ID+`"`))
	require.Len(t, e.x.ctl.created.Attachments, 2)
	assert.Equal(t, a.ID, e.x.ctl.created.Attachments[0].ID)
	assert.Equal(t, b.ID, e.x.ctl.created.Attachments[1].ID)

	// A task without any still works.
	require.Equal(t, http.StatusCreated, post(``))
	assert.Empty(t, e.x.ctl.created.Attachments)

	assert.Equal(t, http.StatusBadRequest, post(`"nope"`))
	assert.Equal(t, http.StatusBadRequest, post(`"`+a.ID+`","`+b.ID+`","`+c.ID+`"`), "over the cap")
	assert.Equal(t, http.StatusBadRequest, post(`"`+a.ID+`","`+a.ID+`"`))
}
