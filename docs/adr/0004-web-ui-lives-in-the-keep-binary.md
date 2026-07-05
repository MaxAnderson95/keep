# The web UI lives in the keep binary as `keep serve`

**Status:** accepted

## Context

keep needs a web management interface usable from a phone over Tailscale: status, logs, and the
lifecycle verbs without SSH. The orchestration layer (`internal/keep.Manager`) was built as the
single seam the CLI and TUI are thin shells over — but it sits under `internal/`, so a separate
project cannot import it and would have to shell out to `keep --json`/`keep up …`, a stringly-typed
integration that loses typed errors and couples to CLI output. keep also ships as a single static
binary via goreleaser (D15), and the whole point of the Config model is one artifact, one version.

## Decision

The web UI is a subcommand of the existing binary: `keep serve` (default `127.0.0.1:4098`). It is
a third thin shell over `Manager`, beside `internal/cli` and `internal/tui`. The frontend is a
Vite+ SPA (React + TypeScript + Tailwind) in `web/`, built to `web/dist` (gitignored) and embedded
with `go:embed`; GoReleaser builds it in a before-hook. Serve exposes a first-class JSON API at
`/api/v1` reusing the D24 JSON-tagged result structs.

`keep serve` is itself declared in the Config as a resident Service (`keep-web`), so keep manages
its own UI. To handle the self-reference, `keep fork` sets `KEEP_SERVICE=<name>` in every
Service's environment; serve reads its own to identify itself and refuses `down` on that Service
(bounce stays allowed — launchd restarts residents, so it self-heals).

## Why

- Direct `Manager` calls: typed results, no subprocess parsing, no version skew between UI and CLI.
- One artifact: goreleaser, the pinned-path model (D15), and the Config entry stay trivial —
  `command: keep serve`.
- Dogfooding: the UI runs through the same fork launcher, env model, and verbs it manages.

## Consequences

- keep gains its **first long-lived process**. The invoke-and-exit assumption breaks, so serve
  reloads the Config from disk on every request — semantically a CLI invocation per request; an
  invalid mid-edit Config degrades the UI to read-only instead of crashing.
- A node toolchain (Vite+, pinned — it is v0.1.x) enters the repo and the release pipeline; bare
  `go build` requires the frontend built once first. Accepted because installs come from GitHub
  releases, not `go install`.
- The binary grows by the embedded SPA (immaterial at this scale).
- Downing the serve Service from its own UI is blocked; recovery for a genuinely wedged serve is
  iPhone SSH + CLI.
