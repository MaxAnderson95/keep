package keep

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// LogTarget is one log stream (stdout or stderr) for a Service.
type LogTarget struct {
	Service string
	Stream  string // "out" | "err"
	Path    string
}

// LogTargets returns the log streams for the named Services (all if empty).
func (m *Manager) LogTargets(names []string) ([]LogTarget, error) {
	targets, err := m.Targets(names)
	if err != nil {
		return nil, err
	}
	var out []LogTarget
	for _, s := range targets {
		out = append(out,
			LogTarget{Service: s.Name, Stream: "out", Path: m.Cfg.StdoutPath(s)},
			LogTarget{Service: s.Name, Stream: "err", Path: m.Cfg.StderrPath(s)},
		)
	}
	return out, nil
}

// TailOnce prints the last n lines of each target. When prefix is true each
// line is tagged with its Service/stream (used when tailing more than one).
func (m *Manager) TailOnce(targets []LogTarget, n int, w io.Writer, prefix bool) error {
	for _, t := range targets {
		lines, err := lastLines(t.Path, n)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, line := range lines {
			writeLine(w, t, line, prefix)
		}
	}
	return nil
}

// Follow tails the targets, interleaving appended lines until ctx is canceled.
func (m *Manager) Follow(ctx context.Context, targets []LogTarget, w io.Writer, prefix bool) error {
	offsets := make(map[string]int64, len(targets))
	for _, t := range targets {
		if fi, err := os.Stat(t.Path); err == nil {
			offsets[t.Path] = fi.Size()
		}
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for _, t := range targets {
				offsets[t.Path] = m.drain(t, offsets[t.Path], w, prefix)
			}
		}
	}
}

func (m *Manager) drain(t LogTarget, off int64, w io.Writer, prefix bool) int64 {
	f, err := os.Open(t.Path)
	if err != nil {
		return off
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return off
	}
	if fi.Size() < off {
		off = 0 // truncated (rotation) — restart from the top
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return off
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		writeLine(w, t, sc.Text(), prefix)
	}
	return fi.Size()
}

func writeLine(w io.Writer, t LogTarget, line string, prefix bool) {
	if prefix {
		fmt.Fprintf(w, "%s[%s] %s\n", t.Service, t.Stream, line)
		return
	}
	fmt.Fprintln(w, line)
}

// lastLines returns the final n lines of a file.
func lastLines(path string, n int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := splitLines(data)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func splitLines(data []byte) []string {
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		return nil
	}
	parts := bytes.Split(data, []byte("\n"))
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = string(p)
	}
	return out
}
