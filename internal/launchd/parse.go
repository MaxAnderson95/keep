package launchd

import (
	"regexp"
	"strconv"
	"strings"
)

// PrintInfo is the subset of `launchctl print` keep cares about.
type PrintInfo struct {
	Loaded      bool
	State       string // e.g. "running", "waiting", "not running"
	PID         int    // 0 when not running
	HasPID      bool
	LastExit    int
	HasLastExit bool
	Path        string // plist path launchd loaded
}

var (
	statePrintRE = regexp.MustCompile(`(?m)^\s*state\s*=\s*(.+?)\s*$`)
	pidPrintRE   = regexp.MustCompile(`(?m)^\s*pid\s*=\s*(\d+)\s*$`)
	exitPrintRE  = regexp.MustCompile(`(?m)^\s*last exit code\s*=\s*(.+?)\s*$`)
	pathPrintRE  = regexp.MustCompile(`(?m)^\s*path\s*=\s*(.+?)\s*$`)
)

// ParsePrint extracts PrintInfo from `launchctl print` output.
func ParsePrint(out string, loaded bool) PrintInfo {
	info := PrintInfo{Loaded: loaded}
	if !loaded {
		return info
	}
	if m := statePrintRE.FindStringSubmatch(out); m != nil {
		info.State = m[1]
	}
	if m := pidPrintRE.FindStringSubmatch(out); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			info.PID = n
			info.HasPID = true
		}
	}
	if m := exitPrintRE.FindStringSubmatch(out); m != nil {
		v := strings.TrimSpace(m[1])
		if n, err := strconv.Atoi(v); err == nil {
			info.LastExit = n
			info.HasLastExit = true
		}
	}
	if m := pathPrintRE.FindStringSubmatch(out); m != nil {
		info.Path = m[1]
	}
	return info
}

var disabledLineRE = regexp.MustCompile(`"([^"]+)"\s*=>\s*(disabled|enabled)`)

// ParseDisabled parses `launchctl print-disabled` output into a map of
// label -> disabled?.
func ParseDisabled(out string) map[string]bool {
	res := map[string]bool{}
	for _, m := range disabledLineRE.FindAllStringSubmatch(out, -1) {
		res[m[1]] = m[2] == "disabled"
	}
	return res
}
