package systemd

import (
	"path/filepath"
	"strconv"
	"strings"
)

// showProps is the parsed subset of `systemctl show` keep cares about.
type showProps struct {
	LoadState      string
	ActiveState    string
	SubState       string
	MainPID        int
	ExecMainStatus int
	HasExitStatus  bool
	Result         string
}

// parseShow reads `systemctl show -p <key>` output, which is one KEY=VALUE per
// line. systemd omits nothing it was asked for, but an unknown property comes
// back empty, so every field stays optional.
func parseShow(out string) showProps {
	var p showProps
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			p.LoadState = value
		case "ActiveState":
			p.ActiveState = value
		case "SubState":
			p.SubState = value
		case "MainPID":
			if n, err := strconv.Atoi(value); err == nil {
				p.MainPID = n
			}
		case "ExecMainStatus":
			if n, err := strconv.Atoi(value); err == nil {
				p.ExecMainStatus = n
				p.HasExitStatus = true
			}
		case "Result":
			p.Result = value
		}
	}
	return p
}

// parseUnitFiles reads `systemctl list-unit-files --no-legend --no-pager`
// output into stem -> disabled?. Each line is "<unit> <state> [preset]".
//
// A scheduled Service has both a .timer and a .service under one stem, and
// only the .timer is ever enabled or disabled, so the timer's state wins.
func parseUnitFiles(out string) map[string]bool {
	disabled := map[string]bool{}
	fromTimer := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		unit, state := fields[0], fields[1]
		ext := filepath.Ext(unit)
		if ext != serviceExt && ext != timerExt {
			continue
		}
		stem := strings.TrimSuffix(unit, ext)
		if ext == serviceExt && fromTimer[stem] {
			continue
		}
		disabled[stem] = state == "disabled"
		if ext == timerExt {
			fromTimer[stem] = true
		}
	}
	return disabled
}

// parseVersion reads the major version from `systemctl --version` output,
// whose first line is like "systemd 255 (255.4-1ubuntu8)". It returns 0 when
// the version cannot be read.
func parseVersion(out string) int {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "systemd" {
			continue
		}
		// The token may carry a suffix (e.g. "255~rc1"); take the leading
		// digits.
		digits := fields[1]
		for i, c := range digits {
			if c < '0' || c > '9' {
				digits = digits[:i]
				break
			}
		}
		if n, err := strconv.Atoi(digits); err == nil {
			return n
		}
	}
	return 0
}

// readMarkers parses keep's [X-Keep] section out of a unit file. Unit files are
// INI-shaped: sections in brackets, KEY=VALUE lines, `#` and `;` comments.
func readMarkers(data []byte) (managed bool, service, keepPath string) {
	inMarker := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inMarker = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]") == MarkerSection
			continue
		}
		if !inMarker {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case MarkerKey:
			managed = strings.TrimSpace(value) == "true"
		case ServiceKey:
			service = strings.TrimSpace(value)
		case KeepPathKey:
			keepPath = strings.TrimSpace(value)
		}
	}
	return managed, service, keepPath
}
