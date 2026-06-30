# Config is the single source of truth; launchd files are generated artifacts

**Status:** accepted

## Context

keep replaces a hand-written pattern where each service was a wrapper script + a launchd plist +
a zsh control function, with the generated files committed to `~/dotfiles` and symlinked into
`~/Library/LaunchAgents`. That versioned derived files and required a manual relink step on a new
machine.

## Decision

Only the declarative keep **Config** is version-controlled (in `~/dotfiles`). `keep apply`
renders plists as disposable **generated artifacts** written directly into `~/Library/LaunchAgents`
(there are no wrapper scripts — `keep fork` is the launcher; see ADR-0002). Generated artifacts
are never hand-edited and never committed, and can be regenerated from the Config at any time.

## Why

A single authoritative source eliminates the per-service boilerplate, reduces a fresh-Mac rebuild
to "clone dotfiles → `keep apply`", and makes drift detection (declared Config vs live launchd
state) possible.

## Considered options

- **Track generated files + symlink (the current pattern).** Rejected: versions derived files and
  keeps the manual relink step.
- **Hybrid — track both, always regenerate.** Rejected: blurs which artifact is authoritative.

## Consequences

- Generated artifacts are throwaway — a stray hand-edit in `~/Library/LaunchAgents` will be
  clobbered by the next `apply` (and ideally flagged as drift).
- keep owns writing into `~/Library/LaunchAgents` directly, rather than going through dotfiles
  symlinks.
