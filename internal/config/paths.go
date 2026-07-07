package config

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultConfigPath is where keep looks for the Config unless overridden by
// --config or the KEEP_CONFIG environment variable.
func DefaultConfigPath() string {
	if env := os.Getenv("KEEP_CONFIG"); env != "" {
		return ExpandPath(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "keep", "config.yaml")
	}
	return filepath.Join(home, ".config", "keep", "config.yaml")
}

// DefaultLogDir is the convention directory for service logs.
func DefaultLogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("Library", "Logs", "keep")
	}
	return filepath.Join(home, "Library", "Logs", "keep")
}

// StateDir is keep's machine-local state directory (never committed anywhere),
// e.g. the per-service update locks (U8). Serve keeps its own state file here
// too (W6).
func StateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("Library", "Application Support", "keep")
	}
	return filepath.Join(home, "Library", "Application Support", "keep")
}

// LaunchAgentsDir is the user LaunchAgents directory where generated artifacts live.
func LaunchAgentsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("Library", "LaunchAgents")
	}
	return filepath.Join(home, "Library", "LaunchAgents")
}

// ExpandPath expands a leading ~ (or ~/...) to the user's home directory.
// It performs no $VAR interpolation — keep deliberately has no interpolation.
func ExpandPath(p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
