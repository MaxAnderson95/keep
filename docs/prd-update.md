# Keep Update — Product Requirements (Draft)

> **Status:** Draft — shaped via a grilling session on 2026-07-06.
> Scope: the `keep update` verb, its Config fields, and its API/web surfaces. The core product
> PRD lives in [../PRD.md](../PRD.md); glossary in [../CONTEXT.md](../CONTEXT.md); the
> load-bearing stop/fail semantics are [ADR-0006](./adr/0006-update-reuses-down-up-and-fails-closed.md).

## One-liner

`keep update <service>` is one verb over each tool's own updater: declare the update commands
per Service in the Config, and keep handles the unsafe part — persistently stop the Service,
run the commands with the Service's environment while capturing output, and restore the prior
state only if everything succeeded.

## Why it exists

Most of the tools keep manages ship their own updaters (`opencode upgrade`,
`openchamber upgrade`, …). Running one safely today is a manual dance: `keep down`, re-create
the Service's environment by hand, run the updater, watch the output, `keep up` — and doing it
from the phone means SSH. Encoding the commands in the Config turns that dance into a single
action, available identically from the CLI, the API, and the web UI, with the failure mode
(stop everything, stay stopped, say so loudly) designed in rather than improvised.

## Decisions

| # | Decision | Notes |
|---|----------|-------|
| U1 | **Config: `update:` — a list of command strings** | Each entry tokenized like `command:` (shell-word split in Go, no shell — D16); pipes/conditionals belong in a wrapper script. Run sequentially; the first non-zero exit stops the run. Valid on resident and scheduled Services alike. |
| U2 | **Verb: `keep update <svc>` — exactly one Service** | No multi-target or bare-all form in v1 (deliberate divergence from up/down/bounce). Naming a Service that declares no update commands is an error. |
| U3 | **Stop is the real Down; start is the real Up; failure = Hold** | The run Downs the Service (disable + bootout) so `apply`/reboot cannot resurrect it mid-update, and brings it Up again only on success. Any failure leaves the Service held Down — fail closed, surfaced by the existing `status`/`diff` drift reporting. No new lifecycle states. See [ADR-0006](./adr/0006-update-reuses-down-up-and-fails-closed.md). |
| U4 | **Prior state is restored; Holds are respected** | A Service already held Down still updates, but a successful run leaves it Down — Update never silently undoes a deliberate `keep down` (ADR-0003). CLI/UI output notes it stayed held. |
| U5 | **Execution context: the Service's env + working_dir** | Env layered like fork (global `env_files` < service `env_files` < global `env` < service `env` — D21) over the invoking process's environment as the base (there is no launchd context here). Runs in the Service's `working_dir`; stdin from `/dev/null`; umask not applied. |
| U6 | **Output: streamed live + appended to `<name>.update.log`** | Combined stdout/stderr streams to the caller as it runs and is appended to `~/Library/Logs/keep/<name>.update.log` (log_dir-relative, like other logs) with a per-run header: timestamp, each command, exit codes, final outcome. Participates in D23 rotation. |
| U7 | **Whole-run timeout: default 10m, `update_timeout` override** | One clock over all commands. Same duration syntax as `interval` (Go durations + `d`); `0` disables. On expiry: SIGTERM the process group, 10s grace, SIGKILL; the run is a failure (→ Hold). |
| U8 | **Concurrency: per-service exclusive flock** | A lock file in keep's machine-local state dir (`~/Library/Application Support/keep/`, per W6) held for the run's duration. A second attempt — from any process, CLI or serve — fails fast with "update in progress". flock self-releases on process death. |
| U9 | **API: detached run + SSE stream** | `POST /api/v1/services/{name}/update` starts the run in a background goroutine and responds with an SSE stream of output events ending in a result event. Client disconnect never cancels a run — a half-replaced binary must not depend on a browser tab. Reattach by streaming the update log. |
| U10 | **Self-update refused over the API** | The run's first step Downs the target; for the Service serving the request that kills the updater mid-run and never comes back Up. Extends the W3 self-down block. Updating the serve Service is a CLI operation (separate process, safe). |
| U11 | **Status: live `updating` state via lock probe** | `status` (and thus CLI, TUI, and web) does a non-blocking probe of the update lock: held ⇒ the Service reports `updating` instead of looking like an unexplained Hold. `keep show` lists the declared update commands. The last run's outcome lives in the update log, not in status. |
| U12 | **Web UI: Update verb button + live output panel** | On the service detail page, an Update button in the VerbBar — rendered only when the Service declares update commands, disabled for the self Service — behind the usual ConfirmSheet. Triggering opens an inline panel streaming the run's SSE, ending in an explicit success / failed (held Down) state. |
| U13 | **Term: Update** | Config field `update:`, verb `keep update`, API path `/update`, glossary entry in CONTEXT.md. _Avoid:_ upgrade (that's the underlying tools' word; `up` + `upgrade` as sibling keep verbs would collide). |

## Command surface

- `keep update <svc>` — exactly one Service: take the lock, Down, run the declared update
  commands sequentially (output streamed and logged), then restore the prior state — Up only if
  the Service wasn't held and every command exited zero. Non-zero exit if any step fails, and
  the Service is left held Down.

## API surface (v1 delta)

| Endpoint | Meaning |
|---|---|
| `POST /api/v1/services/{name}/update` | start a detached run; SSE stream of output + terminal result event; refused for the self Service and while a run holds the lock |

Existing surfaces gain: the `updating` state in status shapes, update commands in `show`, and
access to `<name>.update.log` for reattaching to a running or finished update.

## Example Config (delta)

```yaml
services:
  opencode:
    type: resident
    command: ~/.opencode/bin/opencode serve --hostname 127.0.0.1 --port 4096
    port: 4096
    update:
      - ~/.opencode/bin/opencode upgrade
    # update_timeout: 10m   # whole-run; default 10m; 0 disables
```

## Accepted risks / tradeoffs

- **A serve-started run dies with serve.** The detached run lives in the `keep serve` process;
  if serve is bounced or crashes mid-run, the updater is orphan-killed and the target stays
  held Down (the flock self-releases, so `updating` clears). Recovery: read the update log,
  `keep up` or retry. Acceptable for v1; a survivor process is real complexity (see ADR-0006).
- **Downtime spans the whole run.** Down → update → Up means the Service is unavailable for the
  update's duration, by design — never run a Service on a half-replaced binary. Tools that
  support safe in-place updates could use update-then-Bounce someday; not in v1.
- **No run history.** Outcomes live only in the update log (and the resulting Hold). A failed
  update looks like a deliberate Hold in `status` except for that log.
- **10m default may kill a slow-but-legitimate updater.** `update_timeout` is the escape hatch.
- **No shell.** Update entries can't use pipes, redirects, or conditionals — same tradeoff as
  `command:` (D16); write a wrapper script.

## Deferred (post-v1)

- **Multi-target / bare-all `keep update`** — sequential all-updatable runs with a summary table.
- **Run history / last-update in status** — persisted per-service last run time + outcome.
- **Structured update steps** — per-command env, working_dir, or timeout.
- **TUI trigger** — the TUI inherits the `updating` state via status but gets no update action yet.
- **Update-then-Bounce mode** — zero-downtime path for tools that update safely in place.

## Non-goals

- **Rollback.** keep doesn't snapshot or version the software behind a Service; recovery from a
  bad update is the tool's own concern (or the failed-update Hold plus your hands).
- **Update detection.** keep never checks whether an update is *available* or parses versions;
  it runs your commands when you say so.
- **Automatic updates.** Update is imperative-only. keep will not schedule its own updates —
  nothing stops you declaring a scheduled Service that runs `keep update`, but keep won't grow
  a built-in auto-update loop.
