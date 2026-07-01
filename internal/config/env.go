package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// EnvSource identifies where an assembled env value came from, for `keep show`.
type EnvSource string

const (
	SourceGlobalFile  EnvSource = "global env_file"
	SourceServiceFile EnvSource = "service env_file"
	SourceGlobalMap   EnvSource = "global env"
	SourceServiceMap  EnvSource = "service env"
)

// EnvEntry is one assembled environment variable contributed by keep.
type EnvEntry struct {
	Key    string
	Value  string
	Source EnvSource
	Secret bool // true when the value came from an env_file
}

// KeepEnv assembles the environment keep contributes to a Service — the
// dotenv env_files plus the literal env map — in precedence order (D21):
//
//	global env_files < service env_files < global env < service env
//
// The launchd base environment is intentionally excluded here; it is layered
// in only at fork time. A missing env_file is a hard error.
func (c *Config) KeepEnv(s *Service) ([]EnvEntry, error) {
	idx := map[string]int{}
	var entries []EnvEntry

	set := func(key, val string, src EnvSource, secret bool) {
		if i, ok := idx[key]; ok {
			entries[i] = EnvEntry{Key: key, Value: val, Source: src, Secret: secret}
			return
		}
		idx[key] = len(entries)
		entries = append(entries, EnvEntry{Key: key, Value: val, Source: src, Secret: secret})
	}

	for _, f := range c.Defaults.EnvFiles {
		vars, err := LoadDotenvFile(f)
		if err != nil {
			return nil, fmt.Errorf("global env_file: %w", err)
		}
		for _, v := range vars {
			set(v.Key, v.Value, SourceGlobalFile, true)
		}
	}
	for _, f := range s.EnvFiles {
		vars, err := LoadDotenvFile(f)
		if err != nil {
			return nil, fmt.Errorf("service %q env_file: %w", s.Name, err)
		}
		for _, v := range vars {
			set(v.Key, v.Value, SourceServiceFile, true)
		}
	}
	for _, k := range sortedKeys(c.Defaults.Env) {
		set(k, c.Defaults.Env[k], SourceGlobalMap, false)
	}
	for _, k := range sortedKeys(s.Env) {
		set(k, s.Env[k], SourceServiceMap, false)
	}
	return entries, nil
}

// ForkEnv produces the final environment slice ("KEY=value") for exec, layering
// keep's contributed env over the inherited launchd base environment (D21).
func (c *Config) ForkEnv(s *Service, base []string) ([]string, error) {
	env := map[string]string{}
	order := []string{}
	add := func(k, v string) {
		if _, ok := env[k]; !ok {
			order = append(order, k)
		}
		env[k] = v
	}
	for _, kv := range base {
		if eq := strings.IndexByte(kv, '='); eq >= 0 {
			add(kv[:eq], kv[eq+1:])
		}
	}
	entries, err := c.KeepEnv(s)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		add(e.Key, e.Value)
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+env[k])
	}
	return out, nil
}

// EnvFileRefs returns every env_file path referenced for a Service (global +
// per-service), expanded, for doctor's broken-reference check.
func (c *Config) EnvFileRefs(s *Service) []string {
	var refs []string
	for _, f := range c.Defaults.EnvFiles {
		refs = append(refs, ExpandPath(f))
	}
	for _, f := range s.EnvFiles {
		refs = append(refs, ExpandPath(f))
	}
	return refs
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// OSEnviron is os.Environ, indirected for tests.
var OSEnviron = os.Environ
