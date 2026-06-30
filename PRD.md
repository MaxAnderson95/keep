# Keep — Product Requirements (Draft)

> **Status:** Draft — actively being shaped via a grill-with-docs session.
> This document captures *what* we're building and *why*. The glossary of terms lives in
> [CONTEXT.md](./CONTEXT.md); hard-to-reverse decisions get an ADR in [docs/adr/](./docs/adr/).

## One-liner

`keep` is a rootless, launchd-native CLI for declaring and managing background services on macOS
(long-running or scheduled) — a declarative config generates the launchd plumbing, and one
ergonomic command surface (CLI + TUI) drives the lifecycle.

## Why it exists

Today each background coding service (opencode, openchamber, t3 code, userspace tailscaled, …)
is three hand-written, copy-pasted artifacts — a wrapper script, a launchd plist, and a near-
identical `*-server` zsh function — all symlinked from `~/dotfiles`. As the number of services
grows toward 10+, that boilerplate compounds, and the control verbs (`start`/`stop`/`restart`)
have collapsed into near-duplicates under `KeepAlive`. `keep` replaces the boilerplate with a
declarative model and a clean command surface, while staying launchd-native.

Background and the build-vs-buy survey that led here:
https://guazpmaxagentreports.z13.web.core.windows.net/background-service-manager-discovery.html

## Decisions so far

| # | Decision | Notes |
|---|----------|-------|
| D1 | **Name: `keep`** (binary `keep`) | Round-2 pick over perch/roost/vigil/tend/lares/mews; the launchd `KeepAlive` double-meaning (and `keep down` literally describing a held service) won. Config at `~/.config/keep/`, labels `keep.<name>`, binary `~/.local/bin/keep`. |
| D2 | **Manages only services it declares** | keep owns the full lifecycle of services in its config and ignores all unmanaged launchd jobs. Tight, IaC-style blast radius. A read-only `doctor` over all agents is deferred as a possible later feature. |
| D3 | **Two service types: resident & scheduled** | A Service is either *resident* (KeepAlive, always running) or *scheduled* (calendar/interval, then exits). Implementation may prioritize resident first, but the model accounts for both. "Daemon" is reserved for the launchd mechanism, not a service type. |
| D4 | **Config is the sole source of truth; launchd files are generated** | Only the Config is committed; `keep apply` renders disposable plists into `~/Library/LaunchAgents` (no wrapper scripts — `keep fork` is the launcher). See [ADR-0001](./docs/adr/0001-config-is-source-of-truth.md). |
| D5 | **Single YAML config** at `~/.config/keep/config.yaml` | One file, list/map of services; symlinked from `~/dotfiles`. May add includes/dir-merge later if it grows. |
| D6 | **Launch via hidden `keep fork <svc>`; env assembled at fork time** | plist runs `keep fork`; env = `env_files` (dotenv) + an inline `env:` map of literal non-secret values. Secrets (in env_files) never touch the Config or plist. See [ADR-0002](./docs/adr/0002-keep-fork-launcher-and-secrets.md). |
| D7 | **env_files are dotenv, parsed in Go** | `KEY=val` / `export KEY=val` with comments/quotes; no shell in the exec chain. env_files must be plain assignments, not shell logic. |
| D8 | **Both declarative & imperative** | `keep apply` / `keep diff` reconcile from Config; `keep up\|down\|bounce\|status\|logs [svc...]` for on-the-fly control. |
| D9 | **Verbs: up / down / bounce; down is a persistent hold** | `down` = disable + bootout (reboot-safe, survives apply); `up` = enable + bootstrap; `bounce` = kickstart -k. `apply` respects holds. See [ADR-0003](./docs/adr/0003-verb-semantics-and-persistent-down.md). |
| D10 | **`status` = launchd state + lightweight liveness** | state/PID/uptime/last-exit, drift flagged inline, optional port-listening check via a `port:` hint; scheduled services show last run/exit. No configurable health probes. |
| D11 | **Logs: convention paths + single/combined tail + optional rotation** | keep owns log paths (overridable); `keep logs [svc] -f` tails one or interleaves all with prefixes; optional size/age rotation. _Impl note: rotation likely via a `newsyslog` drop-in or copytruncate, since launchd holds the file handle open._ |
| D12 | **No lifecycle hooks; keep is lifecycle-only** | Tailscale exposure (and any side effects) stay fully external and separately coordinated. Smaller surface; absolute separation. Revisit only if manual re-coordination becomes painful. |
| D13 | **Dedicated `keep doctor`** | Read-only scan aggregating missing binaries, broken env_file refs, orphaned/hand-edited plists, error states, stale keep path, and drift, with suggested fixes. |
| D14 | **TUI in v1; bare `keep` opens it** | `keep` (no args) and `keep tui` launch an interactive dashboard (service list, live state, up/down/bounce, log viewer); `-h`/`--help` always prints help, never the TUI. CLI keeps `--json` on status/diff for scripting + as the TUI's state layer. |
| D15 | **Go + goreleaser, GitHub releases only (no brew yet); MIT** | Single static arm64 binary. No Homebrew tap for now, so path stability is self-managed: `keep apply` stamps keep's own resolved path (`os.Executable()`) into every plist; default home `~/.local/bin/keep` per Max's conventions. urfave/cli (CLI) + bubbletea (TUI). |
| D16 | **Command: `command:` string OR `args:` array, mutually exclusive** | `command:` is shell-word split in Go (quotes respected, no shell spawned); `args:` is explicit argv. Exactly one required (error on both/neither). First token resolved via the service PATH or an absolute path. |
| D17 | **Type defaults to resident; scheduled is explicit** | Omit `type:` → resident. `type: scheduled` required to schedule and must carry a `schedule:`. Validation errors on mismatch (schedule on non-scheduled; scheduled without schedule). |
| D18 | **Scheduling: `calendar` (structured) or `interval` (duration), exactly one** | `calendar:` structured fields (list for multiple fire times) → StartCalendarInterval; `interval: 6h` → StartInterval. Optional cron-string sugar later. |
| D19 | **Label default `keep.<name>`; managed via embedded plist marker** | Default label `keep.<name>` (overridable via `label:`); managed-detection uses a marker key keep writes into each generated plist, independent of the label. |
| D20 | **`enabled: false` (declared off) distinct from `down` (local hold)** | Disambiguated by config flag vs live disable state, no extra keep state: live-disabled+config-enabled = hold (drift); live-disabled+config-disabled = declared off; live-enabled+config-disabled = drift. Declared-off is committed/portable; holds are machine-local. See [ADR-0003](./docs/adr/0003-verb-semantics-and-persistent-down.md). |
| D21 | **Env precedence (confirmed)** | Low→high: launchd base < global `env_files` < per-service `env_files` < global `env` < per-service `env`. |
| D22 | **No interpolation; env = dotenv `env_files` + literal `env:` map** | Dropped `${VAR}` entirely — launchd has no ambient shell env at fork to pull from (it never sources `~/.zshrc`). Secrets go in uncommitted `env_files`; non-secret literals go in the committed `env:` map. |
| D23 | **Log rotation: opportunistic, on keep invocations** | Paths `~/Library/Logs/keep/<name>.{out,err}.log` (global `log_dir` overridable). Rotation off by default; when enabled (size/age thresholds), keep rotates oversized logs whenever you run a command — no scheduled job. Caveat: an untouched resident grows between invocations. Mechanism (copytruncate vs rotate+reopen) settled at build due to the held file handle. |
| D24 | **`--json` on status / diff / show (+ apply summary)** | Read/inspect commands emit structured JSON (scripting, agent workflows, TUI state layer); action verbs (up/down/bounce) stay human-text. |
| D25 | **Rootless, `gui/$UID` domain** | All agents run in the per-user GUI launchd domain — no root/sudo — matching the existing setup's AnyConnect/EDR coexistence constraints. |

## Environment & secrets model

The most important piece. keep is launchd-native (no resident daemon), so the only seam to inject
environment is at exec time, via the hidden `keep fork <service>` launcher (ADR-0002).

Two ways to supply environment, available per-service and globally:

1. **`env_files: [paths]`** — dotenv-format files (`KEY=val` / `export KEY=val`, comments,
   quotes) keep parses in Go and loads at fork time. No shell in the exec chain, so they must be
   plain assignments. Not committed; may hold literal secrets.
2. **`env: { KEY: value }`** — inline map of **literal, non-secret** values only (no
   interpolation). Lives in the committed Config, so never put a secret here — use an `env_file`.

**Assembly order** (later overrides earlier):
launchd base env → global `env_files` → per-service `env_files` → global `env` → per-service `env`.

**Secret story:** a literal secret lives only in an uncommitted `env_file` (loaded at fork); the
committed Config's `env:` map holds only non-secret literals; neither the Config nor the generated
plist ever contains a literal secret. There is no interpolation — env is exactly `env_files` plus
the literal `env:` map (launchd has no ambient `~/.zshrc` environment at fork to interpolate from).

## Command surface (taking shape)

**Declarative**
- `keep apply` — reconcile live launchd state to the Config (generate plists, load/unload). Respects holds.
- `keep diff` — preview what `apply` would change + report drift (incl. held-down services).
- `keep validate` — check Config validity _(tentative)_.

**Imperative (on the fly; accept `[svc...]` or all)**
- `keep up` / `keep down` / `keep bounce` — see D9.
- `keep status [--all]` — state per service (running / idle / held / drifted), PID, uptime, last exit; optional port-listening check via a `port:` hint.
- `keep logs [svc] [-f]` — tail one service, or all interleaved with name prefixes.

**Interactive**
- `keep` (no args) or `keep tui` — open the TUI dashboard. `-h`/`--help` prints help instead.

**Management / introspection**
- `keep show <svc>` — resolved command + environment (secrets masked).
- `keep edit` — open the Config in `$EDITOR` _(tentative)_.
- `keep doctor` — read-only health scan with suggested fixes (D13).
- `keep fork <svc>` — **hidden**, launchd-only launcher (D6).

## Resolved branches

All core branches are resolved (D1–D15): scope, job types, config model, artifact model, env &
secrets, command surface, verbs/stop semantics, drift/reconcile, status, logs, hooks (none), doctor,
TUI, and distribution. Remaining work is config-schema detail — see *Open items* below.

## Example Config (illustrative — schema not final)

```yaml
# ~/.config/keep/config.yaml   (symlinked from ~/dotfiles; the only committed file)

defaults:                         # applied to every service unless overridden
  env_files:
    - ~/.config/keep/secrets.env # dotenv; uncommitted; may hold secrets
  log_dir: ~/Library/Logs/keep   # per-service log files derive from the service name

services:
  opencode:
    type: resident                # resident | scheduled
    command: ~/.opencode/bin/opencode serve --hostname 127.0.0.1 --port 4096
    working_dir: ~
    umask: "077"
    port: 4096                    # enables the status liveness check

  openchamber:
    type: resident
    command: ~/Library/pnpm/openchamber serve --foreground --host 127.0.0.1 --port 4097
    port: 4097
    env_files:
      - ~/.config/keep/openchamber.env   # secrets (e.g. API_TOKEN) — dotenv, uncommitted
    env:                                   # non-secret literals only
      OPENCODE_BINARY: ~/.opencode/bin/opencode
      OPENCODE_PORT: "4096"
      OPENCODE_SKIP_START: "true"

  opencode-db-backup:
    type: scheduled
    command: ~/.local/bin/opencode-db-backup
    schedule:
      calendar: { hour: 2, minute: 30 }   # daily 02:30 (alternatively: interval: 6h)
```

## Deferred (post-v1)

Explicitly out of v1 scope, but designed not to preclude:

- **Homebrew tap** — GitHub releases only for now (D15); a `maxanderson95/tap` would let plists
  pin the stable brew shim path.
- **Cron-string schedule sugar** — `"30 2 * * *"` parsed into structured calendar fields (D18).
- **Config includes / per-service-file merge** — if the single file grows unwieldy (D5).
- **Read-only inspection over all launchd jobs** — a lanchr-style view beyond managed services (D2).
- **Lifecycle hooks** — revisit only if external Tailscale re-coordination becomes painful (D12).

## Non-goals (running list)

- **Managing unmanaged launchd jobs.** keep never controls Apple, vendor, or hand-made agents
  it didn't create. (A read-only inspection view may be reconsidered later.)
- **Lifecycle hooks / built-in side effects.** keep is lifecycle-only; Tailscale exposure and
  anything similar stay external and separately coordinated (D12). Revisit only if it hurts.
