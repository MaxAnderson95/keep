package keep

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MaxAnderson95/keep/internal/config"
)

// RotateLogs performs opportunistic, in-process log rotation (D23). It is
// invoked on keep commands — there is no scheduled job. Rotation is off unless
// the Config enables it; when on, any log exceeding the size and/or age
// threshold is rotated.
//
// Mechanism: copytruncate. launchd holds the StandardOutPath/StandardErrorPath
// file handle open in append mode (O_APPEND), so truncating the file in place
// reclaims its space and the next write appends at the new end-of-file — no
// sparse hole. (A rotate-then-reopen scheme cannot work here: keep is not the
// launchd parent and cannot make launchd reopen its handle.)
//
// RotateLogs is best-effort and returns the rotation actions taken; callers
// generally ignore errors so a rotation hiccup never blocks the real command.
func (m *Manager) RotateLogs() ([]string, error) {
	rot := m.Cfg.Defaults.Rotation
	if !rot.Enabled {
		return nil, nil
	}
	maxBytes, err := parseSize(rot.MaxSize)
	if err != nil {
		return nil, fmt.Errorf("rotation max_size: %w", err)
	}
	var maxAge time.Duration
	if rot.MaxAge != "" {
		secs, err := config.ParseInterval(rot.MaxAge)
		if err != nil {
			return nil, fmt.Errorf("rotation max_age: %w", err)
		}
		maxAge = time.Duration(secs) * time.Second
	}
	keepN := rot.Keep
	if keepN < 1 {
		keepN = 1
	}

	var actions []string
	seen := map[string]bool{}
	for i := range m.Cfg.Services {
		s := &m.Cfg.Services[i]
		for _, path := range []string{m.Cfg.StdoutPath(s), m.Cfg.StderrPath(s)} {
			if seen[path] {
				continue
			}
			seen[path] = true
			rotated, err := rotateFile(path, maxBytes, maxAge, keepN)
			if err != nil {
				return actions, err
			}
			if rotated {
				actions = append(actions, path)
			}
		}
	}
	return actions, nil
}

func rotateFile(path string, maxBytes int64, maxAge time.Duration, keepN int) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if fi.Size() == 0 {
		return false, nil
	}

	overSize := maxBytes > 0 && fi.Size() >= maxBytes
	overAge := maxAge > 0 && time.Since(fi.ModTime()) >= maxAge
	if !overSize && !overAge {
		return false, nil
	}

	// Shift older rotated copies out of the way: .(N-1) -> .N, dropping the
	// oldest beyond keepN.
	for i := keepN - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", path, i)
		to := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	// Copy current content to .1, then truncate the live file in place.
	if err := copyFile(path, path+".1"); err != nil {
		return false, err
	}
	if err := os.Truncate(path, 0); err != nil {
		return false, err
	}
	return true, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// parseSize parses a human size like "10MB", "10M", "500KB", "1GB". An empty
// string returns 0 (size trigger disabled).
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, nil
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "GB"), strings.HasSuffix(s, "G"):
		mult = 1 << 30
		s = strings.TrimSuffix(strings.TrimSuffix(s, "GB"), "G")
	case strings.HasSuffix(s, "MB"), strings.HasSuffix(s, "M"):
		mult = 1 << 20
		s = strings.TrimSuffix(strings.TrimSuffix(s, "MB"), "M")
	case strings.HasSuffix(s, "KB"), strings.HasSuffix(s, "K"):
		mult = 1 << 10
		s = strings.TrimSuffix(strings.TrimSuffix(s, "KB"), "K")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return n * mult, nil
}
