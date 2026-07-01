package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKeepEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	globalFile := writeFile(t, dir, "global.env", "SHARED=from-global-file\nGFILE=g\n")
	svcFile := writeFile(t, dir, "svc.env", "SHARED=from-service-file\nSFILE=s\n")

	cfg := &Config{
		Defaults: Defaults{
			EnvFiles: []string{globalFile},
			Env:      map[string]string{"SHARED": "from-global-map", "GMAP": "gm"},
		},
		Services: []Service{{
			Name:     "x",
			EnvFiles: []string{svcFile},
			Env:      map[string]string{"SHARED": "from-service-map", "SMAP": "sm"},
		}},
	}
	entries, err := cfg.KeepEnv(&cfg.Services[0])
	if err != nil {
		t.Fatalf("KeepEnv: %v", err)
	}
	got := map[string]EnvEntry{}
	for _, e := range entries {
		got[e.Key] = e
	}
	// Highest precedence wins: per-service env map.
	if got["SHARED"].Value != "from-service-map" {
		t.Errorf("SHARED = %q, want from-service-map", got["SHARED"].Value)
	}
	// env_file values are flagged secret; env map values are not.
	if !got["GFILE"].Secret || !got["SFILE"].Secret {
		t.Error("env_file values should be marked secret")
	}
	if got["GMAP"].Secret || got["SMAP"].Secret {
		t.Error("env map values should not be marked secret")
	}
}

func TestForkEnvLayersOverBase(t *testing.T) {
	cfg := &Config{
		Defaults: Defaults{Env: map[string]string{"FOO": "keep"}},
		Services: []Service{{Name: "x", Env: map[string]string{"BAR": "svc"}}},
	}
	base := []string{"PATH=/usr/bin", "FOO=base", "ONLYBASE=1"}
	env, err := cfg.ForkEnv(&cfg.Services[0], base)
	if err != nil {
		t.Fatalf("ForkEnv: %v", err)
	}
	m := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	if m["FOO"] != "keep" {
		t.Errorf("FOO = %q, want keep (keep overrides base)", m["FOO"])
	}
	if m["ONLYBASE"] != "1" {
		t.Errorf("ONLYBASE missing; base env should pass through")
	}
	if m["BAR"] != "svc" {
		t.Errorf("BAR = %q, want svc", m["BAR"])
	}
	if m["PATH"] != "/usr/bin" {
		t.Errorf("PATH = %q, want /usr/bin", m["PATH"])
	}
}

func TestKeepEnvMissingFileErrors(t *testing.T) {
	cfg := &Config{
		Services: []Service{{Name: "x", EnvFiles: []string{"/nonexistent/keep-test.env"}}},
	}
	if _, err := cfg.KeepEnv(&cfg.Services[0]); err == nil {
		t.Fatal("expected error for missing env_file")
	}
}
