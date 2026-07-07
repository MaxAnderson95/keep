package keep

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/MaxAnderson95/keep/internal/config"
)

// ErrUpdateInProgress means another process holds the Service's update lock.
var ErrUpdateInProgress = errors.New("an update is already in progress")

// updateLock is an exclusive per-service flock held for an update run's
// duration (U8). flock releases on process death, so a crashed run can never
// wedge future updates, and a non-blocking probe of the same lock is what
// status reports as "updating" (U11).
type updateLock struct {
	f *os.File
}

func (m *Manager) updateLockPath(name string) string {
	return filepath.Join(m.StateDir(), "update-"+name+".lock")
}

// acquireUpdateLock takes the Service's exclusive update lock, failing fast
// with ErrUpdateInProgress when any process already holds it.
func (m *Manager) acquireUpdateLock(name string) (*updateLock, error) {
	if err := os.MkdirAll(m.StateDir(), 0o755); err != nil {
		return nil, fmt.Errorf("update lock dir: %w", err)
	}
	f, err := os.OpenFile(m.updateLockPath(name), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("update lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrUpdateInProgress
		}
		return nil, fmt.Errorf("update lock: %w", err)
	}
	return &updateLock{f: f}, nil
}

func (l *updateLock) release() {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}

// UpdateInProgress reports whether an update run currently holds the
// Service's lock — a non-blocking shared-lock probe, from any process.
func (m *Manager) UpdateInProgress(s *config.Service) bool {
	f, err := os.Open(m.updateLockPath(s.Name))
	if err != nil {
		return false // never locked (or unreadable): not updating
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return true // exclusive holder present
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}
