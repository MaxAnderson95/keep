package launchd

import "testing"

const samplePrint = `gui/501/keep.web = {
	active count = 1
	path = /Users/me/Library/LaunchAgents/keep.web.plist
	type = LaunchAgent
	state = running

	program = /Users/me/.local/bin/keep
	pid = 4242
	runs = 1
	last exit code = 0
}`

func TestParsePrintRunning(t *testing.T) {
	info := ParsePrint(samplePrint, true)
	if !info.Loaded {
		t.Error("should be loaded")
	}
	if info.State != "running" {
		t.Errorf("state = %q, want running", info.State)
	}
	if !info.HasPID || info.PID != 4242 {
		t.Errorf("pid = %d (has=%v), want 4242", info.PID, info.HasPID)
	}
	if !info.HasLastExit || info.LastExit != 0 {
		t.Errorf("last exit = %d (has=%v), want 0", info.LastExit, info.HasLastExit)
	}
	if info.Path != "/Users/me/Library/LaunchAgents/keep.web.plist" {
		t.Errorf("path = %q", info.Path)
	}
}

func TestParsePrintNeverExited(t *testing.T) {
	out := "state = running\n\tlast exit code = (never exited)\n"
	info := ParsePrint(out, true)
	if info.HasLastExit {
		t.Error("never-exited should not report a last exit code")
	}
}

func TestParsePrintNotLoaded(t *testing.T) {
	info := ParsePrint("", false)
	if info.Loaded {
		t.Error("should not be loaded")
	}
}

func TestParseDisabled(t *testing.T) {
	out := `
	disabled services = {
		"keep.web" => false
		"keep.held" => disabled
		"keep.on" => enabled
		"com.apple.thing" => disabled
	}`
	set := ParseDisabled(out)
	if !set["keep.held"] {
		t.Error("keep.held should be disabled")
	}
	if set["keep.on"] {
		t.Error("keep.on should be enabled")
	}
	if !set["com.apple.thing"] {
		t.Error("com.apple.thing should be disabled")
	}
}
