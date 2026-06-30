# `down` is a persistent hold via launchctl disable; verbs are up / down / bounce

**Status:** accepted

## Context

launchd `KeepAlive` resurrects a `SIGTERM`'d job, so a naive "stop" does not stay stopped — the
core flaw in the previous hand-rolled setup, where `start`, `restart`, and `stop` all collapsed
into "bounce the process" and only `bootout` truly halted a service.

## Decision

A clean, non-overlapping verb set, each mapped to a precise launchctl primitive:

- **`up`** = `launchctl enable` + `bootstrap` (+ `kickstart` if not `RunAtLoad`).
- **`down`** = `launchctl disable` + `bootout` — persists across reboot and survives `apply`,
  until `up`.
- **`bounce`** = `kickstart -k` (restart in place).

`apply` does not resurrect a held-down (disabled) Service; `diff`/`status` report it as drift
("declared enabled, currently held down"). The hold lives in launchd's own disable database —
keep maintains no separate state file.

## Why

Makes "down stays down" true and reboot-safe with zero keep-side state, and replaces the
collapsed start/restart/stop verbs of the old setup with three that don't overlap.

## Consequences

- A held-down Service is *intentional* drift; `diff`/`status` must distinguish "held down" from
  "broken / unexpectedly stopped".
- Relies on `launchctl enable`/`disable` persistence semantics — to be verified during build.
- A `down` hold (live-disabled while Config says `enabled: true`) is distinct from a declared
  `enabled: false`. keep tells them apart from config-flag vs live state with no extra state; a
  hold is machine-local (not portable), whereas `enabled: false` is committed and survives a
  fresh-Mac `apply`.
