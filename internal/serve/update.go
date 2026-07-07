package serve

import (
	"bytes"
	"context"
	"net/http"

	"github.com/MaxAnderson95/keep/internal/keep"
)

// handleUpdate starts an update run and streams its output as SSE: one
// {"line": ...} event per output line, then a terminal event with the
// UpdateResult (docs/prd-update.md U9). The run is detached — it executes on
// a background context, so a client disconnect never cancels an in-flight
// update; reattach by streaming the service's update log.
//
// The self Service is refused outright (U10): the run's first act is Down,
// which would kill this server mid-run and never bring it back.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.opts.SelfService != "" && name == s.opts.SelfService {
		writeErr(w, http.StatusConflict, "self_update_blocked",
			"refusing to update "+name+": it is the service running this web UI (run `keep update "+name+"` from the CLI instead)")
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
	if !svc.HasUpdate() {
		writeErr(w, http.StatusBadRequest, "no_update_commands",
			"service "+name+" declares no update commands")
		return
	}
	// Fast, friendly refusal; the engine's lock still decides races (U8).
	if o.UpdateInProgress(svc) {
		writeErr(w, http.StatusConflict, "update_in_progress",
			"an update for "+name+" is already running")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	s.log.Info("update started",
		"service", name,
		"tailscale_user", r.Header.Get("Tailscale-User-Login"))

	// context.Background(), NOT r.Context(): the run must survive the client.
	lw := &sseLineWriter{w: w, flusher: flusher}
	res, err := o.Update(context.Background(), svc, lw)
	lw.flushPartial()
	if err != nil && res.Error == "" {
		res.Error = err.Error()
	}
	s.log.Info("update finished", "service", name, "ok", res.OK, "error", res.Error)

	done := updateDoneEvent{Done: true, UpdateResult: res}
	if statuses, err := o.Status([]string{name}); err == nil && len(statuses) == 1 {
		done.Status = &statuses[0]
	}
	writeSSE(w, done)
	flusher.Flush()
}

// updateDoneEvent is the terminal SSE event of an update stream.
type updateDoneEvent struct {
	Done bool `json:"done"`
	keep.UpdateResult
	Status *keep.ServiceStatus `json:"status,omitempty"`
}

// sseLineWriter turns an update run's raw output into per-line SSE events.
// Writes never fail (the engine treats output as best-effort anyway); after
// the client disconnects it degrades to a no-op.
type sseLineWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	buf     []byte
	dead    bool
}

func (lw *sseLineWriter) Write(p []byte) (int, error) {
	if lw.dead {
		return len(p), nil
	}
	lw.buf = append(lw.buf, p...)
	wrote := false
	for {
		i := bytes.IndexByte(lw.buf, '\n')
		if i < 0 {
			break
		}
		lw.emit(string(lw.buf[:i]))
		lw.buf = lw.buf[i+1:]
		wrote = true
	}
	if wrote && !lw.dead {
		lw.flusher.Flush()
	}
	return len(p), nil
}

// flushPartial emits any trailing output that did not end in a newline.
func (lw *sseLineWriter) flushPartial() {
	if lw.dead || len(lw.buf) == 0 {
		return
	}
	lw.emit(string(lw.buf))
	lw.buf = nil
	lw.flusher.Flush()
}

func (lw *sseLineWriter) emit(line string) {
	if err := writeSSE(lw.w, updateLineEvent{Line: line}); err != nil {
		lw.dead = true
	}
}

// updateLineEvent is one line of update run output.
type updateLineEvent struct {
	Line string `json:"line"`
}
