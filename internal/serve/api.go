package serve

import (
	"net/http"

	"github.com/MaxAnderson95/keep/internal/config"
	"github.com/MaxAnderson95/keep/internal/keep"
)

// orch builds a fresh orchestrator for this request, translating a broken
// on-disk Config into a 503 the UI turns into its "Config invalid" banner.
func (s *Server) orch(w http.ResponseWriter) (orchestrator, bool) {
	o, err := s.newOrch()
	if err != nil {
		if isConfigError(err) {
			writeErr(w, http.StatusServiceUnavailable, "config_invalid", err.Error())
		} else {
			writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		}
		return nil, false
	}
	return o, true
}

// resolve maps a path name to its Service; Targets only errors on unknown
// names, which is a 404 here.
func resolve(w http.ResponseWriter, o orchestrator, name string) (*config.Service, bool) {
	targets, err := o.Targets([]string{name})
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown_service", err.Error())
		return nil, false
	}
	return targets[0], true
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":      s.opts.Version,
		"commit":       s.opts.Commit,
		"self_service": s.opts.SelfService,
		"config_path":  s.configPath(),
	})
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	o, ok := s.orch(w)
	if !ok {
		return
	}
	statuses, err := o.Status(nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "status_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": statuses})
}

func (s *Server) handleService(w http.ResponseWriter, r *http.Request) {
	o, ok := s.orch(w)
	if !ok {
		return
	}
	name := r.PathValue("name")
	if _, ok := resolve(w, o, name); !ok {
		return
	}
	statuses, err := o.Status([]string{name})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "status_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, statuses[0])
}

// handleVerb executes up/down/bounce and returns the refreshed status so the
// UI updates instantly (W10). Down on the Service running this server is
// refused outright (W3) — that is a fat-thumb lockout, not a use case.
func (s *Server) handleVerb(verb string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if verb == "down" && s.opts.SelfService != "" && name == s.opts.SelfService {
			writeErr(w, http.StatusConflict, "self_down_blocked",
				"refusing to down "+name+": it is the service running this web UI (use the CLI over SSH instead)")
			return
		}
		o, ok := s.orch(w)
		if !ok {
			return
		}
		svc, ok := resolve(w, o, name)
		if !ok {
			return
		}
		var err error
		switch verb {
		case "up":
			err = o.Up(svc)
		case "down":
			err = o.Down(svc)
		case "bounce":
			err = o.Bounce(svc)
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "verb_failed", verb+" "+name+": "+err.Error())
			return
		}
		s.log.Info("verb executed",
			"verb", verb,
			"service", name,
			"tailscale_user", r.Header.Get("Tailscale-User-Login"))
		statuses, err := o.Status([]string{name})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "status_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": statuses[0]})
	}
}

func (s *Server) handleShow(w http.ResponseWriter, r *http.Request) {
	o, ok := s.orch(w)
	if !ok {
		return
	}
	resolved, err := o.Show(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown_service", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resolved)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	o, ok := s.orch(w)
	if !ok {
		return
	}
	plan, err := o.ComputePlan()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "diff_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	o, ok := s.orch(w)
	if !ok {
		return
	}
	findings, err := o.Doctor()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "doctor_failed", err.Error())
		return
	}
	if findings == nil {
		findings = []keep.Finding{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": findings})
}
