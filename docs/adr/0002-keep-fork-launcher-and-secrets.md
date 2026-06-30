# Services launch via a hidden `keep fork` shim; secrets never enter the plist

**Status:** accepted

## Context

launchd gives a job its environment only via the plist's `EnvironmentVariables` (world-readable
in `~/Library/LaunchAgents`) or the global `launchctl setenv` domain. There is no per-job
env-file hook, and keep has no resident daemon to inject environment the way process-compose,
pm2, and supervisord do — those tools are the spawning parent and build the child's environment
in-process. keep is launchd-native, so its only seam is at exec time.

## Decision

The generated plist's `ProgramArguments` invoke a hidden `keep fork <service>` subcommand —
omitted from `--help`, intended for launchd only, not human use. At launch, `keep fork`:

1. loads the declared `env_files` (dotenv; global, then per-service),
2. applies the inline `env:` map of literal, non-secret values (no interpolation),
3. sets umask and working directory,
4. `exec`s the real command, replacing itself so launchd/KeepAlive tracks the real PID.

The plist itself carries no environment.

## Why

This is the only launchd-native way to make file-based env available to a job without writing it
into the world-readable plist. Secrets live only in uncommitted `env_files` (loaded at fork); the
committed Config's `env:` map holds only non-secret literals. Neither the Config nor the plist
ever contains a literal secret.

There is deliberately **no `${VAR}` interpolation**: launchd does not source your shell rc, so
there is no ambient `~/.zshrc` environment at fork to interpolate from. A job's environment is
exactly `env_files` plus the literal `env:` map.

## Consequences

- Every Service start depends on the keep binary at a pinned absolute path; `keep apply` pins
  it. If keep is missing/moved, Services can't start until the next `apply`.
- The plist shows `keep fork X` rather than the raw command; `keep show <svc>` reveals the
  resolved command and environment (secret values masked).
- Editing an env_file takes effect on the next launch (`keep bounce`), with no plist regenerate.
