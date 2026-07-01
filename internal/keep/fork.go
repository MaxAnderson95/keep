package keep

import (
	"fmt"
	"os"
	"syscall"

	"github.com/MaxAnderson95/keep/internal/config"
)

// Fork is the hidden launchd-only launcher (ADR-0002). It assembles the
// Service's environment, sets umask and working directory, then execs the real
// command — replacing itself so launchd/KeepAlive tracks the real PID.
//
// Fork only returns on failure; on success it never returns (the process image
// is replaced). Errors are surfaced to the Service's stderr log.
func (m *Manager) Fork(name string) error {
	s, ok := m.Cfg.Service(name)
	if !ok {
		return fmt.Errorf("fork: unknown service %q", name)
	}

	argv, err := s.ResolveArgv()
	if err != nil {
		return fmt.Errorf("fork %q: %w", name, err)
	}

	env, err := m.Cfg.ForkEnv(s, config.OSEnviron())
	if err != nil {
		return fmt.Errorf("fork %q: %w", name, err)
	}

	bin, err := resolveExecutable(argv[0], pathFromEnv(env))
	if err != nil {
		return fmt.Errorf("fork %q: cannot resolve command %q: %w", name, argv[0], err)
	}

	if mask, ok, err := s.ParsedUmask(); err != nil {
		return fmt.Errorf("fork %q: %w", name, err)
	} else if ok {
		syscall.Umask(mask)
	}

	if s.WorkingDir != "" {
		if err := os.Chdir(config.ExpandPath(s.WorkingDir)); err != nil {
			return fmt.Errorf("fork %q: working_dir: %w", name, err)
		}
	}

	// Replace this process with the real command (same PID).
	argv[0] = bin
	if err := syscall.Exec(bin, argv, env); err != nil {
		return fmt.Errorf("fork %q: exec %s: %w", name, bin, err)
	}
	return nil // unreachable on success
}
