package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/bobbyrc/onett/internal/config"
)

// Config editing is owner-only (see apiRoutes). The file is the source of truth: every save
// writes the file, and the daemon then reloads it. See docs/proposals/accounts-and-config-editor.md.

// configFile is the body of GET /config/file.
type configFile struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Text    string `json:"text"`
}

// configDocument is the body of GET /config/document. The document is the file as written, before
// ${NAME} expansion, so references show as written and never as their values.
type configDocument struct {
	Path     string            `json:"path"`
	Version  string            `json:"version"`
	Document any               `json:"document"`
	Schema   config.SchemaNode `json:"schema"`
	// DefaultRouting is the built-in agent for each task kind, which a kind falls back to when
	// the file drops its route.
	DefaultRouting map[string]string `json:"default_routing"`
}

// configSaved is the body of PUT /config/file and PATCH /config/document.
type configSaved struct {
	Version         string   `json:"version"`
	RestartRequired []string `json:"restart_required"`
}

// readConfigFile returns the config file's path and current bytes.
func (s *server) readConfigFile() (string, []byte, error) {
	path := s.opts.Controller.Config().Path
	text, err := os.ReadFile(path)
	if err != nil {
		return path, nil, fmt.Errorf("read config: %w", err)
	}
	return path, text, nil
}

func (s *server) getConfigFile(w http.ResponseWriter, r *http.Request) {
	path, text, err := s.readConfigFile()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, configFile{Path: path, Version: config.Version(text), Text: string(text)})
}

func (s *server) getConfigDocument(w http.ResponseWriter, r *http.Request) {
	path, text, err := s.readConfigFile()
	if err != nil {
		s.fail(w, err)
		return
	}
	doc, err := config.DocumentTree(text)
	if err != nil {
		writeErrorProblems(w, http.StatusUnprocessableEntity, "the config is not valid YAML", config.ProblemsFrom(err))
		return
	}
	writeJSON(w, http.StatusOK, configDocument{
		Path:     path,
		Version:  config.Version(text),
		Document: doc,
		Schema:   config.Schema(),

		DefaultRouting: config.DefaultRouting(),
	})
}

func (s *server) putConfigFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version string  `json:"version"`
		Text    *string `json:"text"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if in.Text == nil {
		s.fail(w, invalidf("text is required"))
		return
	}
	text := []byte(*in.Text)
	s.saveConfig(w, r, in.Version, func([]byte) ([]byte, error) { return text, nil })
}

func (s *server) patchConfigDocument(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version string          `json:"version"`
		Changes []config.Change `json:"changes"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	s.saveConfig(w, r, in.Version, func(current []byte) ([]byte, error) {
		return config.ApplyChanges(current, in.Changes)
	})
}

// saveConfig is the one path every config save takes. It refuses a save based on a version other
// than the file's current one, builds the new text with edit, validates it exactly as a load does,
// writes it atomically and reloads the daemon. Saves run one at a time.
func (s *server) saveConfig(w http.ResponseWriter, r *http.Request, version string, edit func(current []byte) ([]byte, error)) {
	s.saveConfigAs(w, r, &version, edit, func(saved configSaved, _ *config.Config) any { return saved })
}

// saveConfigAs is saveConfig with two differences for callers that edit the file themselves
// (add and remove a project): version nil skips the stale-version check, since the edit is made
// against whatever the file holds now, and reply builds the success body from the saved result
// and the config as parsed. An edit that returns ErrConflict is a 409, and one that returns a
// *config.InvalidResultError is a 422 with the problems.
func (s *server) saveConfigAs(w http.ResponseWriter, r *http.Request, version *string,
	edit func(current []byte) ([]byte, error), reply func(configSaved, *config.Config) any) {
	s.configMu.Lock()
	defer s.configMu.Unlock()

	path, current, err := s.readConfigFile()
	if err != nil {
		s.fail(w, err)
		return
	}
	if version != nil && config.Version(current) != *version {
		writeErrorMsg(w, http.StatusConflict, "the config file changed since you opened it; reload and try again")
		return
	}
	text, err := edit(current)
	var invalid *config.InvalidResultError
	switch {
	case errors.Is(err, ErrConflict):
		writeErrorMsg(w, http.StatusConflict, err.Error())
		return
	case errors.As(err, &invalid):
		writeErrorProblems(w, http.StatusUnprocessableEntity, "the config is not valid", config.ProblemsFrom(invalid.Err))
		return
	case err != nil:
		writeErrorMsg(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	cfg, err := config.Parse(text, path)
	if err != nil {
		writeErrorProblems(w, http.StatusUnprocessableEntity, "the config is not valid", config.ProblemsFrom(err))
		return
	}
	old := s.opts.Controller.Config()
	if err := config.WriteFileAtomic(path, text); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.opts.Controller.ReloadConfig(r.Context()); err != nil {
		s.log.Error("api: reload after config save", "err", err)
		writeErrorMsg(w, http.StatusInternalServerError,
			"the config was saved, but the daemon could not reload it: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reply(configSaved{
		Version:         config.Version(text),
		RestartRequired: nonNil(config.RestartRequired(old, cfg)),
	}, cfg))
}

// restartBlocker returns why a restart would leave no daemon running, or "". A restart onto a
// server.addr that another process holds fails to listen: the re-exec path then exits and
// nothing serves, and under launchd the service crash-loops. So when the config file now names a
// different address from the one this process is bound to, it is tried with a throwaway listen
// first. The address already in use by this process is skipped: it frees when the restart starts.
func (s *server) restartBlocker() string {
	if s.opts.ListenAddr == "" {
		return ""
	}
	_, text, err := s.readConfigFile()
	if err != nil {
		return ""
	}
	cfg, err := config.Parse(text, s.opts.Controller.Config().Path)
	if err != nil || cfg.Server.Addr == s.opts.ListenAddr {
		return ""
	}
	l, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		return fmt.Sprintf("not restarting: server.addr %s cannot be bound (%v); fix server.addr in the config, then restart", cfg.Server.Addr, err)
	}
	_ = l.Close()
	return ""
}

// restart answers 202 and then asks the process to restart itself. The request is already
// answered when the restart starts, and the HTTP server's shutdown waits for this handler to
// return, so the reply reaches the browser before the listener closes.
func (s *server) restart(w http.ResponseWriter, r *http.Request) {
	if s.opts.Restart == nil {
		writeErrorMsg(w, http.StatusNotImplemented, "this daemon cannot restart itself")
		return
	}
	if msg := s.restartBlocker(); msg != "" {
		writeErrorMsg(w, http.StatusConflict, msg)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
	s.opts.Restart()
}
