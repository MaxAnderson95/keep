// Package config defines keep's declarative Config — the single source of
// truth (ADR-0001) — and the parse, validate, and environment-assembly logic
// that turns it into something keep can apply.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Service types.
const (
	TypeResident  = "resident"
	TypeScheduled = "scheduled"
)

// Config is the parsed, validated declarative Config.
type Config struct {
	Defaults Defaults
	Services []Service // sorted by Name for deterministic output
	Path     string    // where this Config was loaded from
}

// Defaults are applied to every Service unless overridden per-service.
type Defaults struct {
	EnvFiles []string
	Env      map[string]string
	LogDir   string
	Rotation Rotation
}

// Rotation controls opportunistic log rotation (D23). Off by default.
type Rotation struct {
	Enabled bool
	MaxSize string // e.g. "10MB"; empty disables the size trigger
	MaxAge  string // e.g. "168h" or "7d"; empty disables the age trigger
	Keep    int    // rotated copies to retain (default 1)
}

// Service is a single declared background process keep manages end-to-end.
type Service struct {
	Name       string
	Type       string // resident | scheduled
	Command    string // shell-word split in Go; mutually exclusive with Args
	Args       []string
	WorkingDir string
	Umask      string
	Port       int
	Label      string // overrides the default keep.<name>
	Enabled    *bool  // nil == true; false == declared off (D20)
	EnvFiles   []string
	Env        map[string]string
	Schedule   *Schedule
	LogDir     string // per-service override
	StdoutPath string // per-service override of the full path
	StderrPath string // per-service override of the full path
}

// Schedule describes when a scheduled Service fires (D18). Exactly one of
// Calendar or Interval is set.
type Schedule struct {
	Calendar []CalendarInterval
	Interval string // a Go duration ("6h", "30m"); also accepts a "d" day suffix
}

// CalendarInterval is one structured fire time. A nil field means "any".
type CalendarInterval struct {
	Minute  *int
	Hour    *int
	Day     *int
	Weekday *int
	Month   *int
}

// IsEnabled reports whether the Service is declared on. Absent == on.
func (s *Service) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// EffectiveLabel is the launchd label for the Service (default keep.<name>).
func (s *Service) EffectiveLabel() string {
	if s.Label != "" {
		return s.Label
	}
	return "keep." + s.Name
}

// IsScheduled reports whether this is a scheduled Service.
func (s *Service) IsScheduled() bool {
	return s.Type == TypeScheduled
}

// Service returns the named Service.
func (c *Config) Service(name string) (*Service, bool) {
	for i := range c.Services {
		if c.Services[i].Name == name {
			return &c.Services[i], true
		}
	}
	return nil, false
}

// ResolveLogDir returns the effective log directory for a Service.
func (c *Config) ResolveLogDir(s *Service) string {
	switch {
	case s.LogDir != "":
		return ExpandPath(s.LogDir)
	case c.Defaults.LogDir != "":
		return ExpandPath(c.Defaults.LogDir)
	default:
		return DefaultLogDir()
	}
}

// StdoutPath returns the resolved stdout log path for a Service.
func (c *Config) StdoutPath(s *Service) string {
	if s.StdoutPath != "" {
		return ExpandPath(s.StdoutPath)
	}
	return filepath.Join(c.ResolveLogDir(s), s.Name+".out.log")
}

// StderrPath returns the resolved stderr log path for a Service.
func (c *Config) StderrPath(s *Service) string {
	if s.StderrPath != "" {
		return ExpandPath(s.StderrPath)
	}
	return filepath.Join(c.ResolveLogDir(s), s.Name+".err.log")
}

// raw* mirror the on-disk YAML shape before normalization.
type rawConfig struct {
	Defaults rawDefaults           `yaml:"defaults"`
	Services map[string]rawService `yaml:"services"`
}

type rawDefaults struct {
	EnvFiles []string          `yaml:"env_files"`
	Env      map[string]string `yaml:"env"`
	LogDir   string            `yaml:"log_dir"`
	Rotation rawRotation       `yaml:"rotation"`
}

type rawRotation struct {
	Enabled bool   `yaml:"enabled"`
	MaxSize string `yaml:"max_size"`
	MaxAge  string `yaml:"max_age"`
	Keep    int    `yaml:"keep"`
}

type rawService struct {
	Type       string            `yaml:"type"`
	Command    string            `yaml:"command"`
	Args       []string          `yaml:"args"`
	WorkingDir string            `yaml:"working_dir"`
	Umask      string            `yaml:"umask"`
	Port       int               `yaml:"port"`
	Label      string            `yaml:"label"`
	Enabled    *bool             `yaml:"enabled"`
	EnvFiles   []string          `yaml:"env_files"`
	Env        map[string]string `yaml:"env"`
	Schedule   *rawSchedule      `yaml:"schedule"`
	LogDir     string            `yaml:"log_dir"`
	StdoutPath string            `yaml:"stdout_path"`
	StderrPath string            `yaml:"stderr_path"`
}

// rawSchedule accepts schedule.calendar as either a single mapping or a list
// of mappings, and schedule.interval as a duration string.
type rawSchedule struct {
	Calendar yaml.Node `yaml:"calendar"`
	Interval string    `yaml:"interval"`
}

// Load reads, parses, and validates the Config at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	cfg.Path = path
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Parse decodes Config bytes into a normalized Config (without validation).
func Parse(data []byte) (*Config, error) {
	var raw rawConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}

	cfg := &Config{
		Defaults: Defaults{
			EnvFiles: raw.Defaults.EnvFiles,
			Env:      raw.Defaults.Env,
			LogDir:   raw.Defaults.LogDir,
			Rotation: Rotation{
				Enabled: raw.Defaults.Rotation.Enabled,
				MaxSize: raw.Defaults.Rotation.MaxSize,
				MaxAge:  raw.Defaults.Rotation.MaxAge,
				Keep:    raw.Defaults.Rotation.Keep,
			},
		},
	}
	if cfg.Defaults.Rotation.Keep == 0 {
		cfg.Defaults.Rotation.Keep = 1
	}

	names := make([]string, 0, len(raw.Services))
	for name := range raw.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		rs := raw.Services[name]
		svc := Service{
			Name:       name,
			Type:       rs.Type,
			Command:    rs.Command,
			Args:       rs.Args,
			WorkingDir: rs.WorkingDir,
			Umask:      rs.Umask,
			Port:       rs.Port,
			Label:      rs.Label,
			Enabled:    rs.Enabled,
			EnvFiles:   rs.EnvFiles,
			Env:        rs.Env,
			LogDir:     rs.LogDir,
			StdoutPath: rs.StdoutPath,
			StderrPath: rs.StderrPath,
		}
		if svc.Type == "" {
			svc.Type = TypeResident // default (D17)
		}
		if rs.Schedule != nil {
			sched, err := normalizeSchedule(name, rs.Schedule)
			if err != nil {
				return nil, err
			}
			svc.Schedule = sched
		}
		cfg.Services = append(cfg.Services, svc)
	}
	return cfg, nil
}

func normalizeSchedule(svc string, rs *rawSchedule) (*Schedule, error) {
	sched := &Schedule{Interval: rs.Interval}
	if rs.Calendar.Kind == 0 {
		return sched, nil
	}
	switch rs.Calendar.Kind {
	case yaml.MappingNode:
		var ci rawCalendar
		if err := rs.Calendar.Decode(&ci); err != nil {
			return nil, fmt.Errorf("service %q: schedule.calendar: %w", svc, err)
		}
		sched.Calendar = []CalendarInterval{ci.toInterval()}
	case yaml.SequenceNode:
		var list []rawCalendar
		if err := rs.Calendar.Decode(&list); err != nil {
			return nil, fmt.Errorf("service %q: schedule.calendar: %w", svc, err)
		}
		for _, ci := range list {
			sched.Calendar = append(sched.Calendar, ci.toInterval())
		}
	default:
		return nil, fmt.Errorf("service %q: schedule.calendar must be a mapping or a list of mappings", svc)
	}
	return sched, nil
}

type rawCalendar struct {
	Minute  *int `yaml:"minute"`
	Hour    *int `yaml:"hour"`
	Day     *int `yaml:"day"`
	Weekday *int `yaml:"weekday"`
	Month   *int `yaml:"month"`
}

func (c rawCalendar) toInterval() CalendarInterval {
	return CalendarInterval{
		Minute:  c.Minute,
		Hour:    c.Hour,
		Day:     c.Day,
		Weekday: c.Weekday,
		Month:   c.Month,
	}
}
