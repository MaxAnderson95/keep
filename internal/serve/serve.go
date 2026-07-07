// Package serve is keep's web management interface: a phone-first SPA plus a
// first-class JSON API at /api/v1, both thin shells over the orchestration in
// internal/keep (ADR-0004). It authenticates at the application level with a
// password, WebAuthn passkeys, and a bearer token (ADR-0005), on top of
// whatever network gating (Tailscale grants) sits in front of it.
package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Options configures a Server. Password or Token may be empty individually,
// but New rejects the combination where both are empty (the UI would be
// unreachable and no passkey could ever be registered).
type Options struct {
	ConfigPath  string // Config to load per request ("" = keep's default)
	Version     string // build version, surfaced in /api/v1/meta
	Commit      string // build commit, surfaced in /api/v1/meta
	Host        string // listen address, e.g. 127.0.0.1
	Port        int    // listen port
	Password    string // KEEP_SERVE_PASSWORD; browser login
	Token       string // KEEP_SERVE_TOKEN; bearer auth for scripting
	SelfService string // KEEP_SERVICE; the Service running this server, "" if none
	StatePath   string // state file path ("" = DefaultStatePath)
	StaticFS    fs.FS  // built SPA (nil = API-only, UI reports not built)
	Logger      *slog.Logger
}

// Server is the keep serve HTTP server.
type Server struct {
	opts       Options
	log        *slog.Logger
	store      *StateStore
	codec      sessionCodec
	ceremonies *ceremonyStore
	newOrch    func() (orchestrator, error)
}

// New validates opts, opens (creating if needed) the state file, and returns
// a Server ready to ListenAndServe.
func New(opts Options) (*Server, error) {
	if opts.Password == "" && opts.Token == "" {
		return nil, errors.New("serve requires KEEP_SERVE_PASSWORD (and optionally KEEP_SERVE_TOKEN) in its environment")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	statePath := opts.StatePath
	if statePath == "" {
		p, err := DefaultStatePath()
		if err != nil {
			return nil, err
		}
		statePath = p
	}
	store, err := OpenStateStore(statePath)
	if err != nil {
		return nil, fmt.Errorf("opening serve state: %w", err)
	}
	s := &Server{
		opts:       opts,
		log:        opts.Logger,
		store:      store,
		codec:      sessionCodec{key: store.SigningKey()},
		ceremonies: newCeremonyStore(),
	}
	s.newOrch = s.defaultOrchestrator
	return s, nil
}

// Handler returns the full HTTP handler: /api/v1 plus the embedded SPA.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Auth surface. Login endpoints are necessarily unauthenticated.
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/state", s.handleAuthState)
	mux.HandleFunc("GET /api/v1/auth/me", s.handleMe)
	mux.HandleFunc("POST /api/v1/auth/passkeys/login/begin", s.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /api/v1/auth/passkeys/login/finish", s.handlePasskeyLoginFinish)
	mux.Handle("GET /api/v1/auth/passkeys", s.authed(s.handlePasskeyList))
	mux.Handle("POST /api/v1/auth/passkeys/register/begin", s.authed(s.handlePasskeyRegisterBegin))
	mux.Handle("POST /api/v1/auth/passkeys/register/finish", s.authed(s.handlePasskeyRegisterFinish))
	mux.Handle("DELETE /api/v1/auth/passkeys/{id}", s.authed(s.handlePasskeyDelete))

	// Read surface (W1).
	mux.Handle("GET /api/v1/meta", s.authed(s.handleMeta))
	mux.Handle("GET /api/v1/services", s.authed(s.handleServices))
	mux.Handle("GET /api/v1/services/{name}", s.authed(s.handleService))
	mux.Handle("GET /api/v1/services/{name}/logs", s.authed(s.handleLogsTail))
	mux.Handle("GET /api/v1/services/{name}/logs/stream", s.authed(s.handleLogsStream))
	mux.Handle("GET /api/v1/services/{name}/show", s.authed(s.handleShow))
	mux.Handle("GET /api/v1/diff", s.authed(s.handleDiff))
	mux.Handle("GET /api/v1/doctor", s.authed(s.handleDoctor))

	// Verbs (W1): up / down / bounce. No apply, no config editing.
	mux.Handle("POST /api/v1/services/{name}/up", s.authed(s.handleVerb("up")))
	mux.Handle("POST /api/v1/services/{name}/down", s.authed(s.handleVerb("down")))
	mux.Handle("POST /api/v1/services/{name}/bounce", s.authed(s.handleVerb("bounce")))

	// Update (docs/prd-update.md): detached run, SSE output stream.
	mux.Handle("POST /api/v1/services/{name}/update", s.authed(s.handleUpdate))

	// Unknown API paths must 404 as JSON, not fall through to the SPA.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not_found", "no such endpoint")
	})

	// Everything else is the SPA.
	mux.Handle("/", s.staticHandler())

	return s.logRequests(mux)
}

// ListenAndServe runs the server until ctx is canceled, then drains in-flight
// requests. WriteTimeout stays 0 so SSE log streams are never cut off.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := net.JoinHostPort(s.opts.Host, fmt.Sprintf("%d", s.opts.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	s.log.Info("keep serve listening", "addr", addr, "self_service", s.opts.SelfService)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
