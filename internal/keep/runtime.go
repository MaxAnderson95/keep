package keep

import (
	"fmt"
	goruntime "runtime"

	"github.com/MaxAnderson95/keep/internal/launchd"
	"github.com/MaxAnderson95/keep/internal/runtime"
)

// detectRuntime picks the service runtime adapter for this machine. It is the
// one place in the orchestration layer that names a concrete runtime: adapters
// import internal/runtime for its types, so the choice cannot live down there
// without an import cycle (ADR-0008).
func detectRuntime() (runtime.Runtime, error) {
	switch goruntime.GOOS {
	case "darwin":
		return launchd.New(), nil
	default:
		return nil, fmt.Errorf("unsupported OS %q: keep has no service runtime adapter for it", goruntime.GOOS)
	}
}
