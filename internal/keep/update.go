package keep

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/MaxAnderson95/keep/internal/config"
)

// ErrNoUpdateCommands means the Service declares no update commands (U2).
var ErrNoUpdateCommands = errors.New("no update commands declared")

// updateKillGrace is how long a timed-out (or canceled) update command gets
// between SIGTERM and SIGKILL (U7). Variable for tests.
var updateKillGrace = 10 * time.Second

// UpdateResult is the outcome of an update run. It doubles as the API's
// terminal SSE event payload.
type UpdateResult struct {
	Service    string `json:"service"`
	OK         bool   `json:"ok"`
	StayedHeld bool   `json:"stayed_held"`         // succeeded but left Down (prior Hold / declared off)
	TimedOut   bool   `json:"timed_out,omitempty"` // the whole-run timeout expired
	Error      string `json:"error,omitempty"`
}

// Update refreshes the software behind a Service (docs/prd-update.md,
// ADR-0006): take the per-service lock, Down, run the declared update
// commands sequentially, and restore the prior state — Up again only if the
// Service wasn't held and every command exited zero. Any failure leaves the
// Service held Down.
//
// Combined output (keep's own progress lines plus the commands'
// stdout/stderr) streams to out and is appended to the Service's update log.
// Both are best-effort: a dead writer never fails the run (U9). ctx
// cancellation is treated like a timeout: the running command is killed and
// the Service stays Down.
func (m *Manager) Update(ctx context.Context, s *config.Service, out io.Writer) (UpdateResult, error) {
	res := UpdateResult{Service: s.Name}
	if !s.HasUpdate() {
		res.Error = ErrNoUpdateCommands.Error()
		return res, ErrNoUpdateCommands
	}

	lock, err := m.acquireUpdateLock(s.Name)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	defer lock.release()

	logw := m.openUpdateLog(s)
	defer logw.Close()
	w := io.MultiWriter(&bestEffort{w: out}, &bestEffort{w: logw})

	fail := func(err error) (UpdateResult, error) {
		res.Error = err.Error()
		fmt.Fprintf(w, "==> update %s: FAILED: %v — service left down (retry, or `keep up %s`)\n", s.Name, err, s.Name)
		return res, err
	}

	// Pre-flight everything that can fail cheaply — argv, env, timeout —
	// before any downtime.
	argvs := make([][]string, len(s.Update))
	for i, cmd := range s.Update {
		argv, err := config.SplitCommand(cmd)
		if err != nil || len(argv) == 0 {
			if err == nil {
				err = errors.New("empty command")
			}
			return fail(fmt.Errorf("update[%d]: %w", i, err))
		}
		argvs[i] = argv
	}
	env, err := m.Cfg.ForkEnv(s, config.OSEnviron())
	if err != nil {
		return fail(err)
	}
	timeout, err := s.UpdateTimeoutDuration()
	if err != nil {
		return fail(err)
	}

	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()
	fmt.Fprintf(w, "==> update %s: %d command(s), timeout %s, %s\n",
		s.Name, len(argvs), timeoutLabel(timeout), start.Format(time.RFC3339))

	// Prior state decides what "restore" means (U4): a Service that was held
	// (or declared off) updates but stays Down.
	disabled, err := m.ctl.DisabledSet()
	if err != nil {
		return fail(fmt.Errorf("reading launchd disable state: %w", err))
	}
	wasDown := disabled[s.EffectiveLabel()] || !s.IsEnabled()

	fmt.Fprintf(w, "==> down %s\n", s.Name)
	if err := m.Down(s); err != nil {
		return fail(fmt.Errorf("down: %w", err))
	}

	for i, argv := range argvs {
		fmt.Fprintf(w, "--> [%d/%d] %s\n", i+1, len(argvs), s.Update[i])
		cmdStart := time.Now()
		err := m.runUpdateCommand(runCtx, s, argv, env, w)
		elapsed := time.Since(cmdStart).Round(100 * time.Millisecond)
		if err != nil {
			if runCtx.Err() != nil {
				res.TimedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded)
				if res.TimedOut {
					fmt.Fprintf(w, "--> TIMED OUT after %s (killed)\n", timeoutLabel(timeout))
				} else {
					fmt.Fprintf(w, "--> CANCELED after %s (killed)\n", elapsed)
				}
				return fail(fmt.Errorf("update[%d]: %w", i, runCtx.Err()))
			}
			fmt.Fprintf(w, "--> FAILED in %s: %v\n", elapsed, err)
			return fail(fmt.Errorf("update[%d]: %w", i, err))
		}
		fmt.Fprintf(w, "--> ok in %s\n", elapsed)
	}

	if wasDown {
		res.StayedHeld = true
		res.OK = true
		fmt.Fprintf(w, "==> update %s: success in %s; stays down (was held before the update)\n",
			s.Name, time.Since(start).Round(100*time.Millisecond))
		return res, nil
	}
	fmt.Fprintf(w, "==> up %s\n", s.Name)
	if err := m.Up(s); err != nil {
		return fail(fmt.Errorf("up: %w", err))
	}
	res.OK = true
	fmt.Fprintf(w, "==> update %s: success in %s\n", s.Name, time.Since(start).Round(100*time.Millisecond))
	return res, nil
}

// runUpdateCommand executes one update command with the Service's env and
// working_dir, stdin from /dev/null, in its own process group. On ctx
// expiry the whole group gets SIGTERM, a grace period, then SIGKILL.
func (m *Manager) runUpdateCommand(ctx context.Context, s *config.Service, argv []string, env []string, w io.Writer) error {
	argv = append([]string(nil), argv...)
	argv[0] = config.ExpandPath(argv[0])
	bin, err := resolveExecutable(argv[0], pathFromEnv(env))
	if err != nil {
		return err
	}
	argv[0] = bin

	cmd := &exec.Cmd{
		Path:   bin,
		Args:   argv,
		Env:    env,
		Stdout: w,
		Stderr: w,
		// New process group so a kill reaches the command's children too.
		SysProcAttr: &syscall.SysProcAttr{Setpgid: true},
	}
	if s.WorkingDir != "" {
		cmd.Dir = config.ExpandPath(s.WorkingDir)
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		pgid := cmd.Process.Pid // == pgid, thanks to Setpgid
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(updateKillGrace):
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			<-done
		}
		return ctx.Err()
	}
}

// openUpdateLog opens the Service's update log for appending. Failure to
// record output must never fail the run, so errors degrade to a no-op writer.
func (m *Manager) openUpdateLog(s *config.Service) io.WriteCloser {
	if err := m.ensureLogDir(s); err != nil {
		return nopWriteCloser{}
	}
	f, err := os.OpenFile(m.Cfg.UpdateLogPath(s), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nopWriteCloser{}
	}
	return f
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

// bestEffort makes a writer infallible: after the first write error (e.g. a
// disconnected SSE client) it silently discards — output capture must never
// abort an in-flight update (U9).
type bestEffort struct {
	w io.Writer
}

func (b *bestEffort) Write(p []byte) (int, error) {
	if b.w == nil {
		return len(p), nil
	}
	if _, err := b.w.Write(p); err != nil {
		b.w = nil
	}
	return len(p), nil
}

func timeoutLabel(d time.Duration) string {
	if d <= 0 {
		return "none"
	}
	return d.String()
}
