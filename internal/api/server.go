// Package api is onett's HTTP surface: the JSON API under /api/v1, the event stream, and the
// embedded web UI. Handlers read from the store and act through a Controller.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/secrets"
	"github.com/bobbyrc/onett/internal/store"
)

// Options configures NewServer.
type Options struct {
	Store      store.Store
	Bus        events.Bus
	Controller Controller
	// Token is the bearer token that the CLI and the orchestrator agent send. It can read and
	// operate, but never edit the config or manage users; see internal/auth.
	Token   string
	Version string
	// SecureCookies sets the Secure flag on the session cookie. serve sets it when public_url is
	// https. A request that arrives over TLS gets the flag either way.
	SecureCookies bool
	// AllowedHosts are the Host names accepted beyond localhost, 127.0.0.1 and ::1, which are
	// always accepted. serve computes the list: the host of server.addr (not a wildcard
	// address), the host of server.public_url and server.allowed_hosts. A request for any other
	// Host gets 421. When the server listens on 0.0.0.0 or ::, no name is added for the bind
	// address, so remote access needs the name listed in server.allowed_hosts.
	AllowedHosts []string
	// AllowedHostsFunc, when set, is asked on every request instead of using AllowedHosts, so
	// a config reload changes the guard without a restart.
	AllowedHostsFunc func() []string
	// SecretsPath is the secrets.env file the owner edits from the UI. Empty turns the secrets
	// routes off (they answer 404).
	SecretsPath string
	// Web is the UI to serve. Nil serves the build embedded from webdist (see DefaultWeb).
	Web fs.FS
	// Secrets is the secrets.env store behind /secrets. Nil makes those routes answer 501.
	Secrets *secrets.Store
	// ChannelTest sends a test message through the running bot of a channel entry; see
	// channel.Supervisor.Test. Nil makes POST /channels/test answer 501.
	ChannelTest func(ctx context.Context, ch config.Channel) error
	// Restart asks the process to shut down gracefully and start again; see POST /restart. It
	// must return at once (serve signals its main loop). Nil makes the route answer 501.
	Restart func()
	// ListenAddr is the address this process is bound to. POST /restart probes a different
	// server.addr in the config file before restarting. Empty skips the probe.
	ListenAddr string
	Logger     *slog.Logger
}

type server struct {
	opts Options
	log  *slog.Logger
	web  fs.FS
	// streams holds one slot per open /events stream; see events in sse.go.
	streams chan struct{}
	// limiter counts failed sign-ins per username; see authLogin.
	limiter *auth.Limiter
	// now is the clock for sessions. Tests may replace it before serving requests.
	now func() time.Time
	// startedAt is when this server was built; /health reports it so a client can tell a
	// restarted daemon from the one it asked to restart.
	startedAt time.Time
	// configMu serialises saves of the config file, so two saves cannot both check the version
	// and then overwrite each other.
	configMu sync.Mutex
}

// NewServer returns the handler for every onett HTTP route.
func NewServer(o Options) http.Handler {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	web := o.Web
	if web == nil {
		web = DefaultWeb()
	}
	s := &server{opts: o, log: o.Logger, web: web, streams: make(chan struct{}, maxEventStreams), now: time.Now, startedAt: time.Now().UTC()}
	s.limiter = auth.NewLimiter(signInLimit, signInWindow, s.now)

	r := chi.NewRouter()
	r.Use(securityHeaders)
	r.Use(middleware.Recoverer)
	r.Use(s.requestLog)
	static := allowedHostSet(o.AllowedHosts)
	hosts := func() map[string]bool { return static }
	if o.AllowedHostsFunc != nil {
		hosts = func() map[string]bool { return allowedHostSet(o.AllowedHostsFunc()) }
	}
	r.Use(hostGuard(hosts))
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(noStore)
		r.NotFound(notFound)
		r.MethodNotAllowed(methodNotAllowed)
		// Outside the principal middleware: health, and the routes that sign a person in.
		r.Get("/health", s.health)
		r.Get("/auth/state", s.authState)
		r.Post("/auth/setup", s.authSetup)
		r.Post("/auth/login", s.authLogin)
		r.Group(func(r chi.Router) {
			r.Use(s.principal, clampLimits)
			r.Post("/auth/logout", s.authLogout)
			r.Post("/auth/password", s.changePassword)
			s.apiRoutes(r)
		})
	})
	// Any other /api/ path is a JSON 404 rather than the web UI.
	r.With(noStore).HandleFunc("/api/*", notFound)
	r.Handle("/*", http.HandlerFunc(s.serveWeb))
	return r
}

// requestLog writes one line per request. Event streams and health checks log at debug level
// so they do not drown the log.
func (s *server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelInfo
		if r.URL.Path == "/api/v1/events" || r.URL.Path == "/api/v1/ws" || r.URL.Path == "/api/v1/health" {
			level = slog.LevelDebug
		}
		s.log.Log(r.Context(), level, "http request",
			"method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(start))
	})
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.opts.Version, "started_at": s.startedAt.Format(time.RFC3339Nano)})
}

// fail writes the error response for err.
func (s *server) fail(w http.ResponseWriter, err error) {
	writeError(w, s.log, err)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeErrorMsg(w, http.StatusNotFound, "not found")
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeErrorMsg(w, http.StatusMethodNotAllowed, "method not allowed")
}
