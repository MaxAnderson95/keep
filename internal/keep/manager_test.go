package keep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaxAnderson95/keep/internal/config"
)

func residentCfg() *config.Config {
	cfg, _ := config.Parse([]byte(`
services:
  web:
    command: /usr/bin/true
    port: 4096
`))
	return cfg
}

func TestBuildJobProgramArgumentsDefaultConfig(t *testing.T) {
	cfg := residentCfg()
	// Empty Path is treated as the default config (no --config stamped).
	m := &Manager{Cfg: cfg, KeepPath: "/Users/me/.local/bin/keep", Version: "1.0.0"}
	job, err := m.BuildJob(&cfg.Services[0])
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/Users/me/.local/bin/keep", "fork", "web"}
	if len(job.ProgramArguments) != 3 {
		t.Fatalf("ProgramArguments = %#v, want %#v", job.ProgramArguments, want)
	}
	for i := range want {
		if job.ProgramArguments[i] != want[i] {
			t.Fatalf("ProgramArguments = %#v, want %#v", job.ProgramArguments, want)
		}
	}
	if !job.RunAtLoad || !job.KeepAlive {
		t.Error("resident job must set RunAtLoad and KeepAlive")
	}
	if job.KeepPath != "/Users/me/.local/bin/keep" {
		t.Errorf("KeepPath marker = %q", job.KeepPath)
	}
}

func TestBuildJobStampsNonDefaultConfig(t *testing.T) {
	cfg := residentCfg()
	cfg.Path = "/tmp/custom-keep.yaml"
	m := &Manager{Cfg: cfg, KeepPath: "/keep", Version: "1.0.0"}
	job, _ := m.BuildJob(&cfg.Services[0])
	joined := strings.Join(job.ProgramArguments, " ")
	if !strings.Contains(joined, "--config /tmp/custom-keep.yaml fork web") {
		t.Errorf("ProgramArguments = %q, want --config stamped", joined)
	}
}

func TestPlistBytesIdempotent(t *testing.T) {
	cfg := residentCfg()
	m := &Manager{Cfg: cfg, KeepPath: "/keep", Version: "1.0.0"}
	a, err := m.PlistBytes(&cfg.Services[0])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := m.PlistBytes(&cfg.Services[0])
	if string(a) != string(b) {
		t.Fatal("PlistBytes not deterministic")
	}
}

func TestShowMasksSecrets(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "secret.env")
	if err := os.WriteFile(envFile, []byte("API_TOKEN=supersecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse([]byte(`
services:
  api:
    command: /usr/bin/true
    env_files:
      - ` + envFile + `
    env:
      PUBLIC_FLAG: "true"
`))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{Cfg: cfg, KeepPath: "/keep", Version: "1.0.0"}
	r, err := m.Show("api")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Env {
		switch e.Key {
		case "API_TOKEN":
			if e.Value == "supersecret" {
				t.Error("secret value leaked in show output")
			}
			if !e.Secret {
				t.Error("API_TOKEN should be marked secret")
			}
		case "PUBLIC_FLAG":
			if e.Value != "true" {
				t.Errorf("non-secret PUBLIC_FLAG = %q, want true", e.Value)
			}
		}
	}
}

func TestScheduledJobBuild(t *testing.T) {
	cfg, err := config.Parse([]byte(`
services:
  backup:
    type: scheduled
    command: /usr/bin/true
    schedule:
      interval: 6h
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Cfg: cfg, KeepPath: "/keep", Version: "1.0.0"}
	job, err := m.BuildJob(&cfg.Services[0])
	if err != nil {
		t.Fatal(err)
	}
	if job.StartInterval != 6*3600 {
		t.Errorf("StartInterval = %d, want %d", job.StartInterval, 6*3600)
	}
	if job.RunAtLoad || job.KeepAlive {
		t.Error("scheduled job must not set RunAtLoad/KeepAlive")
	}
}
