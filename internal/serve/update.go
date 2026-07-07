package serve

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/MaxAnderson95/keep/internal/keep"
)

// handleUpdate starts an update run and streams its output as SSE: one
// {"line": ...} event per output line, then a terminal event with the
// UpdateResult (docs/prd-update.md U9). The run executes in a background
// goroutine on a detached context; this handler goroutine owns the
// ResponseWriter, relaying output lines and heartbeating (like the log
// stream) so an idle proxy doesn't drop the connection during a silent
// update command. A client disconnect never cancels the run — the handler
// keeps draining until the run completes and everything still lands in the
// update log; reattach by streaming that log.
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

	// The run writes lines to a channel from its own goroutine; only this
	// goroutine touches w (the exec copier and a ping ticker writing to the
	// same ResponseWriter would race). context.Background(), NOT
	// r.Context(): the run must survive the client.
	lines := make(chan string, 64)
	outcome := make(chan updateOutcome, 1)
	go func() {
		lw := &lineChanWriter{ch: lines}
		res, err := o.Update(context.Background(), svc, lw)
		lw.flushPartial()
		close(lines)
		outcome <- updateOutcome{res: res, err: err}
	}()

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	dead := false // client gone; keep draining, stop writing
	for lines != nil {
		select {
		case line, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
			if dead {
				continue
			}
			if err := writeSSE(w, updateLineEvent{Line: line}); err != nil {
				dead = true
				continue
			}
			flusher.Flush()
		case <-ping.C:
			if dead {
				continue
			}
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				dead = true
				continue
			}
			flusher.Flush()
		}
	}

	out := <-outcome
	res := out.res
	if out.err != nil && res.Error == "" {
		res.Error = out.err.Error()
	}
	s.log.Info("update finished", "service", name, "ok", res.OK, "error", res.Error)

	done := updateDoneEvent{Done: true, UpdateResult: res}
	if statuses, err := o.Status([]string{name}); err == nil && len(statuses) == 1 {
		done.Status = &statuses[0]
	}
	if !dead {
		_ = writeSSE(w, done)
		flusher.Flush()
	}
}

// updateOutcome carries a finished run's result to the handler goroutine.
type updateOutcome struct {
	res keep.UpdateResult
	err error
}

// updateDoneEvent is the terminal SSE event of an update stream.
type updateDoneEvent struct {
	Done bool `json:"done"`
	keep.UpdateResult
	Status *keep.ServiceStatus `json:"status,omitempty"`
}

// lineChanWriter splits an update run's raw output into complete lines on a
// channel. Sends may block briefly, but never indefinitely: the handler
// drains the channel until the run closes it, even after a client
// disconnect. Writes never error (the engine treats output as best-effort
// anyway).
type lineChanWriter struct {
	ch  chan<- string
	buf []byte
}

func (lw *lineChanWriter) Write(p []byte) (int, error) {
	lw.buf = append(lw.buf, p...)
	for {
		i := bytes.IndexByte(lw.buf, '\n')
		if i < 0 {
			break
		}
		lw.ch <- string(lw.buf[:i])
		lw.buf = lw.buf[i+1:]
	}
	return len(p), nil
}

// flushPartial emits any trailing output that did not end in a newline.
func (lw *lineChanWriter) flushPartial() {
	if len(lw.buf) == 0 {
		return
	}
	lw.ch <- string(lw.buf)
	lw.buf = nil
}

// updateLineEvent is one line of update run output.
type updateLineEvent struct {
	Line string `json:"line"`
}
