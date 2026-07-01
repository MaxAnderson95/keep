package launchd

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// CLI is the production Controller adapter: it shells out to launchctl (and ps
// for uptime) and parses the output via ParsePrint/ParseDisabled.
type CLI struct {
	// run executes launchctl; an internal seam so the adapter's own wiring is
	// not hard-bound to exec. Not exposed — keep's tests use the fake adapter
	// at the Controller interface instead.
	run func(args ...string) (string, error)
}

// NewCLI returns the launchctl-backed Controller.
func NewCLI() *CLI {
	return &CLI{run: execLaunchctl}
}

func execLaunchctl(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// Bootstrap loads a plist into the GUI domain (idempotent: an already-loaded
// service is treated as success).
func (c *CLI) Bootstrap(plistPath string) error {
	out, err := c.run("bootstrap", Domain(), plistPath)
	if err != nil {
		if strings.Contains(out, "already bootstrapped") || strings.Contains(out, "service already loaded") {
			return nil
		}
		return fmt.Errorf("bootstrap %s: %s", plistPath, strings.TrimSpace(out))
	}
	return nil
}

// Bootout unloads a service by label (idempotent: not-loaded is success).
func (c *CLI) Bootout(label string) error {
	out, err := c.run("bootout", Target(label))
	if err != nil {
		if strings.Contains(out, "No such process") || strings.Contains(out, "Could not find") {
			return nil
		}
		return fmt.Errorf("bootout %s: %s", label, strings.TrimSpace(out))
	}
	return nil
}

// Enable clears any persistent disable for the service (reverses Disable).
func (c *CLI) Enable(label string) error {
	out, err := c.run("enable", Target(label))
	if err != nil {
		return fmt.Errorf("enable %s: %s", label, strings.TrimSpace(out))
	}
	return nil
}

// Disable persistently disables the service in launchd's disable database;
// this survives reboot and apply until Enable (ADR-0003).
func (c *CLI) Disable(label string) error {
	out, err := c.run("disable", Target(label))
	if err != nil {
		return fmt.Errorf("disable %s: %s", label, strings.TrimSpace(out))
	}
	return nil
}

// Kickstart starts (or, with kill, restarts) a loaded service.
func (c *CLI) Kickstart(label string, kill bool) error {
	args := []string{"kickstart"}
	if kill {
		args = append(args, "-k")
	}
	args = append(args, Target(label))
	out, err := c.run(args...)
	if err != nil {
		return fmt.Errorf("kickstart %s: %s", label, strings.TrimSpace(out))
	}
	return nil
}

// Info returns parsed launchd state for a service label.
func (c *CLI) Info(label string) (PrintInfo, error) {
	out, err := c.run("print", Target(label))
	if err != nil {
		if strings.Contains(out, "Could not find service") || strings.Contains(out, "could not find") {
			return ParsePrint(out, false), nil
		}
		return PrintInfo{}, fmt.Errorf("print %s: %s", label, strings.TrimSpace(out))
	}
	return ParsePrint(out, true), nil
}

// DisabledSet returns the label -> disabled map for the whole domain.
func (c *CLI) DisabledSet() (map[string]bool, error) {
	out, err := c.run("print-disabled", Domain())
	if err != nil {
		return nil, fmt.Errorf("print-disabled: %s", strings.TrimSpace(out))
	}
	return ParseDisabled(out), nil
}

// Uptime returns a human-readable elapsed time for a running PID (via ps), or
// "" if it cannot be determined.
func (c *CLI) Uptime(pid int) string {
	if pid <= 0 {
		return ""
	}
	out, err := exec.Command("ps", "-o", "etime=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
