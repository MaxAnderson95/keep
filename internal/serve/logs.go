package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/MaxAnderson95/keep/internal/keep"
)

const (
	defaultTailLines = 200
	maxTailLines     = 2000
	followInterval   = 300 * time.Millisecond
	pingInterval     = 15 * time.Second
)

// logEvent is one SSE payload: a single log line tagged with its stream.
type logEvent struct {
	Stream string `json:"stream"`
	Line   string `json:"line"`
}

func tailLinesParam(r *http.Request, def int) int {
	n := def
	if v := r.URL.Query().Get("lines"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	return min(n, maxTailLines)
}

func (s *Server) handleLogsTail(w http.ResponseWriter, r *http.Request) {
	o, ok := s.orch(w)
	if !ok {
		return
	}
	targets, err := o.LogTargets([]string{r.PathValue("name")})
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown_service", err.Error())
		return
	}
	n := tailLinesParam(r, defaultTailLines)
	out := map[string][]string{"out": {}, "err": {}}
	for _, t := range targets {
		lines, _, err := tailFile(t.Path, n)
		if err != nil {
			continue // a log that does not exist yet is simply empty
		}
		out[t.Stream] = lines
	}
	writeJSON(w, http.StatusOK, out)
}

// handleLogsStream streams a service's logs as SSE: a backlog of recent lines
// followed by live output. Each event's data is a logEvent JSON object. The
// follow loop reads from the exact offsets the backlog ended at, so no lines
// fall into a gap between tail and follow; truncation (keep's copytruncate
// rotation) resets the offset like `keep logs -f` does.
func (s *Server) handleLogsStream(w http.ResponseWriter, r *http.Request) {
	o, ok := s.orch(w)
	if !ok {
		return
	}
	targets, err := o.LogTargets([]string{r.PathValue("name")})
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown_service", err.Error())
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

	n := tailLinesParam(r, 50)
	followers := make([]*logFollower, 0, len(targets))
	for _, t := range targets {
		lines, size, err := tailFile(t.Path, n)
		if err == nil {
			for _, line := range lines {
				writeSSE(w, logEvent{Stream: t.Stream, Line: line})
			}
		}
		followers = append(followers, &logFollower{target: t, offset: size})
	}
	flusher.Flush()

	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-ticker.C:
			wrote := false
			for _, f := range followers {
				for _, line := range f.drain() {
					writeSSE(w, logEvent{Stream: f.target.Stream, Line: line})
					wrote = true
				}
			}
			if wrote {
				flusher.Flush()
			}
		}
	}
}

func writeSSE(w io.Writer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// logFollower tracks one log file's read position across poll ticks.
type logFollower struct {
	target  keep.LogTarget
	offset  int64
	partial []byte
}

// drain returns the complete lines appended since the last call.
func (f *logFollower) drain() []string {
	fh, err := os.Open(f.target.Path)
	if err != nil {
		return nil
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return nil
	}
	if fi.Size() < f.offset {
		// Truncated in place (copytruncate rotation) — start over.
		f.offset = 0
		f.partial = nil
	}
	if fi.Size() == f.offset {
		return nil
	}
	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(fh)
	if err != nil {
		return nil
	}
	f.offset += int64(len(data))
	f.partial = append(f.partial, data...)
	var lines []string
	for {
		i := bytes.IndexByte(f.partial, '\n')
		if i < 0 {
			return lines
		}
		lines = append(lines, string(f.partial[:i]))
		f.partial = f.partial[i+1:]
	}
}

// tailFile returns the last n lines of a file plus the file size the read
// covered, so a follower can pick up exactly where the tail ended.
func tailFile(path string, n int) ([]string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	size := int64(len(data))
	trimmed := bytes.TrimRight(data, "\n")
	if len(trimmed) == 0 {
		return nil, size, nil
	}
	parts := bytes.Split(trimmed, []byte("\n"))
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	lines := make([]string, len(parts))
	for i, p := range parts {
		lines[i] = string(p)
	}
	return lines, size, nil
}
