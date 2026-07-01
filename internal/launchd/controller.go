package launchd

// Controller is the seam over the live launchd domain (gui/$UID): the launchctl
// operations keep performs and the state it reads back. It deals in parsed
// results (PrintInfo, the disabled set), not raw launchctl text, so callers and
// tests cross the same interface.
//
// Two adapters satisfy it: CLI (production — shells out to launchctl/ps) and an
// in-memory fake used by keep's tests. That second adapter is what makes the
// orchestration layer (apply, diff, status, doctor, verbs) testable through its
// own interface rather than only against a live machine.
type Controller interface {
	// Bootstrap loads a plist into the domain (idempotent).
	Bootstrap(plistPath string) error
	// Bootout unloads a service by label (idempotent).
	Bootout(label string) error
	// Enable clears a persistent disable for the label (reverses Disable).
	Enable(label string) error
	// Disable persistently disables the label (survives reboot and apply).
	Disable(label string) error
	// Kickstart starts, or with kill restarts, a loaded service.
	Kickstart(label string, kill bool) error
	// Info returns parsed launchd state for a label.
	Info(label string) (PrintInfo, error)
	// DisabledSet returns the label -> disabled map for the whole domain.
	DisabledSet() (map[string]bool, error)
	// Uptime returns a human-readable elapsed time for a running PID, or "".
	Uptime(pid int) string
}
