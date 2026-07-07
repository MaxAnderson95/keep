# keep

A rootless, launchd-native CLI for declaring and managing background services on macOS —
long-running or scheduled. One declarative config generates the launchd plumbing; one ergonomic
command surface (CLI + TUI) drives the lifecycle.

> **Status:** design complete, implementation in progress. The build is tracked in GitHub issues;
> the design lives in [PRD.md](./PRD.md), the glossary in [CONTEXT.md](./CONTEXT.md), and the
> load-bearing decisions in [docs/adr/](./docs/adr/).

## Why

Running background dev services on macOS today means hand-writing a wrapper script, a launchd
plist, and a control function for each one, then symlinking them around. `keep` replaces that
boilerplate with a single declarative config and verbs that actually behave:

- `keep apply` / `keep diff` — reconcile launchd state from your config. `keep` manages only the
  services you declare; everything else on the machine is left untouched.
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

## Design highlights

- The config is the single source of truth; plists are disposable, generated artifacts.
- Services launch via a hidden `keep fork` shim that assembles the environment (dotenv
  `env_files` + a literal `env:` map) and execs — secrets never enter the world-readable plist.
- Two service types: **resident** (KeepAlive) and **scheduled** (calendar / interval).

See [PRD.md](./PRD.md) for the full design and [docs/adr/](./docs/adr/) for the rationale.

## License

[MIT](./LICENSE)
