# keep

A rootless CLI for declaring and managing background services — long-running or scheduled — on
macOS and Linux. One declarative config generates the service-manager plumbing; one ergonomic
command surface (CLI + TUI) drives the lifecycle.

> **Status:** design complete, implementation in progress. The build is tracked in GitHub issues;
> the design lives in [PRD.md](./PRD.md), the glossary in [CONTEXT.md](./CONTEXT.md), and the
> load-bearing decisions in [docs/adr/](./docs/adr/).

## Why

Running background dev services means hand-writing a wrapper script, a launchd plist or systemd
unit, and a control function for each one, then symlinking them around. `keep` replaces that
boilerplate with a single declarative config and verbs that actually behave:

- `keep apply` / `keep diff` — reconcile the service manager's state from your config. `keep`
  manages only the services you declare; everything else on the machine is left untouched.
- `keep up` / `keep down` / `keep bounce` — start, persistently hold, or restart a service.
  `down` actually stays down (survives reboot and re-apply), unlike a bare `SIGTERM` under
  `KeepAlive`.
- `keep update` — run a service's declared update commands (e.g. `opencode upgrade`) the safe
  way: stop the service, run and capture the updaters, start it again only if they all
  succeeded. See [docs/prd-update.md](./docs/prd-update.md).
- `keep status` / `keep logs` / `keep doctor` — see state (with an optional port-liveness check),
  tail logs, and diagnose problems.
- bare `keep` — open the TUI.
- `keep serve` — a phone-first web UI plus a scriptable JSON API (`/api/v1`) over the same
  verbs, designed to run as a keep service itself and sit behind Tailscale. Password + passkey
  (WebAuthn) + bearer-token auth. See [docs/prd-web.md](./docs/prd-web.md).

## Supported platforms

| OS | Service manager | Generated artifacts |
| --- | --- | --- |
| macOS | launchd, per-user GUI domain | `~/Library/LaunchAgents/keep.<name>.plist` |
| Linux | systemd, per-user manager (`systemctl --user`) | `~/.config/systemd/user/keep.<name>.{service,timer}` |

Nothing needs root on either. Two Linux-specific notes:

- **systemd 240 or newer.** Service output goes to log files, not journald, so `keep logs`,
  rotation, and the web UI behave the same on both OSes. That uses `StandardOutput=append:`,
  which landed in systemd 240. `keep doctor` checks the version.
- **Enable lingering** with `loginctl enable-linger $USER`, or your services stop when you log
  out and never start at boot. keep deliberately does not change this for you; `keep doctor`
  warns when it is off.

An `interval:` schedule is exact on macOS and approximate on Linux: systemd measures the gap from
the start of each run, so a long run pushes the next fire out by its own duration. `calendar:`
schedules mean the same thing on both.

## Design highlights

- The config is the single source of truth; generated units and plists are disposable.
- Services launch via a hidden `keep fork` shim that assembles the environment (dotenv
  `env_files` + a literal `env:` map) and execs — secrets never enter the world-readable
  artifact.
- Two service types: **resident** (restarted forever) and **scheduled** (calendar / interval).
- One OS-neutral seam (`internal/runtime`) with one adapter per service manager, so the CLI, TUI,
  and web UI are identical on both platforms. See [ADR-0008](./docs/adr/0008-runtime-seam.md).

See [PRD.md](./PRD.md) for the full design and [docs/adr/](./docs/adr/) for the rationale.

## License

[MIT](./LICENSE)
