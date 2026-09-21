package keep

import (
	"context"
	"fmt"
	goruntime "runtime"

	"github.com/MaxAnderson95/keep/internal/launchd"
	"github.com/MaxAnderson95/keep/internal/runtime"
	"github.com/MaxAnderson95/keep/internal/systemd"
)

// detectRuntime picks the service runtime adapter for this machine. It is the
// one place in the orchestration layer that names a concrete runtime: adapters
// import internal/runtime for its types, so the choice cannot live down there
// without an import cycle (ADR-0008).
func detectRuntime() (runtime.Runtime, error) {
	switch goruntime.GOOS {
	case "darwin":
		return launchd.New(), nil
	case "linux":
		// systemd only, and only the user manager. The probe is what tells a
		// missing user session apart from a machine that has no systemd at
		// all, and keep can do nothing useful without one.
		rt := systemd.New()
		if err := rt.Probe(context.Background()); err != nil {
			return nil, fmt.Errorf("systemctl --user is not reachable: %w; keep needs a systemd user session "+
				"(log in as this user, or enable lingering with `loginctl enable-linger` and export XDG_RUNTIME_DIR)", err)
		}
		return rt, nil
	default:
		return nil, fmt.Errorf("unsupported OS %q: keep has no service runtime adapter for it", goruntime.GOOS)
	}
}
