package api

import (
	"errors"
	"mime"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/attach"
	"github.com/bobbyrc/onett/internal/store"
)

// uploadOverhead is how much a multipart upload may exceed the file limit with its framing.
const uploadOverhead = 1 << 20

// attachmentView is what an upload answers: the record without the conversation or the path.
type attachmentView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Size int64  `json:"size"`
}

// uploadAttachment stores one file (multipart field "file") for an orchestrator conversation.
// Like editing a message, it is for a signed-in person, not the API token. The file is streamed
// to disk against the size limit, never held in memory.
func (s *server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	if _, ok := userPrincipal(w, r); !ok {
		return
	}
	if s.opts.Attachments == nil {
		writeErrorMsg(w, http.StatusNotImplemented, "attachments are not available")
		return
	}
	c, ok := s.orchestratorConversation(w, r)
	if !ok {
		return
	}
	limit := s.opts.Attachments.MaxSize()
	r.Body = http.MaxBytesReader(w, r.Body, limit+uploadOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		s.fail(w, invalidf("want a multipart/form-data upload with one file in the field \"file\""))
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			s.failUpload(w, err, invalidf("the upload has no \"file\" field"))
			return
		}
		if part.FormName() != "file" {
			continue
		}
		a, err := s.opts.Attachments.Save(r.Context(), c.ID, part.FileName(), part)
		if err != nil {
			s.failUpload(w, err, nil)
			return
		}
		// Only the id, size and type are logged, never the name or the content.
		s.log.Info("chat attachment saved", "attachment", a.ID, "size", a.Size, "mime", a.MIME)
		writeJSON(w, http.StatusCreated, attachmentView{ID: a.ID, Name: a.Name, MIME: a.MIME, Size: a.Size})
		return
	}
}

// failUpload maps an upload error to a status: too large 413, unusable file 400, a malformed or
// missing multipart body fallback (when given) 400, anything else as s.fail does.
func (s *server) failUpload(w http.ResponseWriter, err, fallback error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, attach.ErrTooLarge), errors.As(err, &tooBig):
		writeErrorMsg(w, http.StatusRequestEntityTooLarge, "the file is larger than chat.attachments.max_size")
	case errors.Is(err, attach.ErrInvalid):
		s.fail(w, invalidf("%s", strings.TrimPrefix(err.Error(), attach.ErrInvalid.Error()+": ")))
	case fallback != nil:
		s.fail(w, fallback)
	default:
		s.fail(w, err)
	}
}

// downloadAttachment serves a stored file to a signed-in person. An attachment of another
// conversation is 404. Raster images and PDFs show inline; everything else (text, HTML, SVG,
// unknown types) is forced to download, with nosniff and a sandboxing CSP so a stored file can
// never run as a page.
func (s *server) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	if _, ok := userPrincipal(w, r); !ok {
		return
	}
	if s.opts.Attachments == nil {
		writeErrorMsg(w, http.StatusNotImplemented, "attachments are not available")
		return
	}
	c, ok := s.orchestratorConversation(w, r)
	if !ok {
		return
	}
	a, f, err := s.opts.Attachments.Open(r.Context(), chi.URLParam(r, "aid"))
	if err == nil && a.ConversationID != c.ID {
		if f != nil {
			_ = f.Close()
		}
		err = store.ErrNotFound
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	defer f.Close()

	disposition := "attachment"
	if strings.HasPrefix(a.MIME, "image/") || a.MIME == "application/pdf" {
		disposition = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", a.MIME)
	h.Set("X-Content-Type-Options", "nosniff")
	if a.MIME != "application/pdf" { // a sandboxed frame stops the browser's PDF viewer
		h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	}
	h.Set("Cache-Control", "private, no-cache")
	if v := mime.FormatMediaType(disposition, map[string]string{"filename": a.Name}); v != "" {
		h.Set("Content-Disposition", v)
	} else {
		h.Set("Content-Disposition", disposition)
	}
	http.ServeContent(w, r, "", a.CreatedAt, f)
}
