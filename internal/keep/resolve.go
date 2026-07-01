package keep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaxAnderson95/keep/internal/config"
)

// resolveExecutable resolves a command's first token to an executable path.
// A slash-containing or ~-prefixed token is checked directly; a bare name is
// looked up in pathEnv — the Service's *assembled* PATH, not the ambient
// process PATH. Routing both fork and doctor through this one resolver keeps
// their answer to "where is this binary?" identical (launchd's base PATH is
// minimal, and a Service may set its own PATH via env).
func resolveExecutable(arg0, pathEnv string) (string, error) {
	p := config.ExpandPath(arg0)
	if strings.Contains(p, "/") {
		info, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory", p)
		}
		return p, nil
	}
	if pathEnv == "" {
		pathEnv = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			dir = "."
		}
		cand := filepath.Join(dir, p)
		if info, err := os.Stat(cand); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return cand, nil
		}
	}
	return "", fmt.Errorf("%q not found in PATH", p)
}

// pathFromEnv extracts the PATH value from an assembled env slice ("" if absent).
func pathFromEnv(env []string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			return v
		}
	}
	return ""
}
