# Keep Web UI — Product Requirements (Draft)

> **Status:** Draft — shaped via a grilling session on 2026-07-04.
> Scope: the `keep serve` web management UI and its JSON API. The core product PRD lives in
> [../PRD.md](../PRD.md); glossary in [../CONTEXT.md](../CONTEXT.md); hard-to-reverse decisions
> get an ADR in [adr/](./adr/).

## One-liner

`keep serve` is a phone-first web management interface and first-class JSON API over keep's
orchestration layer — run as a keep Service itself, reached in the browser over Tailscale — so
the lifecycle verbs work from anywhere on the tailnet without SSH.

## Why it exists

Today, controlling keep Services away from the Mac means iPhone SSH + the CLI/TUI. The actual
recurring need is small and urgent: see health at a glance, read logs, and bounce a wedged
service (opencode, tailscaled) from the phone. A browser UI over the existing Tailscale Service
pattern (loopback listen → `svc:*` VIP) serves that need with the tooling already in place, and
a stable API opens scripting (iOS Shortcuts, curl) for free.

## Decisions

| # | Decision | Notes |
|---|----------|-------|
| W1 | **Scope: read + verbs; no apply, no config editing** | Read: status/health/drift, logs (tail + follow), diff, doctor, show (secrets stay masked, per D24 shapes). Write: up / down / bounce only. Apply from a phone means "apply whatever half-edit is on disk" — excluded. Config editing is v3-at-best. |
| W2 | **In-binary subcommand `keep serve`** | A third thin shell over `internal/keep.Manager`, beside cli and tui. `internal/` blocks external import, one artifact keeps goreleaser trivial, and UI version always matches the CLI. See [ADR-0004](./adr/0004-web-ui-lives-in-the-keep-binary.md). |
| W3 | **Self-lockout policy: confirm everything, block self-down** | Every mutating verb confirms; targeting the serve Service itself or tailscaled gets a louder "this cuts off your access" warning. `down` on the serve Service is hard-blocked (fat-thumb lockout has zero upside; recovery is iPhone SSH). Bounce is allowed everywhere — launchd restarts residents, so it self-heals. |
| W4 | **Self-identification via `KEEP_SERVICE`** | `keep fork` sets `KEEP_SERVICE=<name>` in every Service's environment; serve reads its own to know which Service it is. Run manually (outside keep), there is no self to protect. |
| W5 | **App-level auth: password + passkeys + bearer token** | Defense in depth on top of tailnet gating (OpenChamber UI-password pattern, plus WebAuthn). Browser: password once, then registered passkey (Face ID). Scripting: static bearer token. Mutations log the `Tailscale-User-Login` header when present — attribution only (it is spoofable from localhost), never authorization. See [ADR-0005](./adr/0005-app-level-auth-despite-tailnet-gating.md). |
| W6 | **Secrets via env_files, state in Application Support** | `KEEP_SERVE_PASSWORD` / `KEEP_SERVE_TOKEN` arrive through the existing `env_files` mechanism (D6/D22). Passkey credentials + cookie signing key live in `~/Library/Application Support/keep/` — deliberately outside `~/.config/keep` so machine-local state never tangles with the dotfiles repo. |
| W7 | **Stateless signed session cookies (~30d)** | Signed with the persistent key from the state file, so bouncing serve does not log the phone out. No server-side session store. `SameSite=Strict`; mutating endpoints also enforce Origin/`Sec-Fetch-Site` checks (CSRF). Bearer-token requests carry no cookies and are exempt. |
| W8 | **First-class scriptable API at `/api/v1`** | Documented, stable endpoint + schema contract from day one; shapes reuse the existing JSON-tagged structs (`ServiceStatus`, `Plan`, `Finding`, …) from D24. Supported consumers: the SPA, curl, iOS Shortcuts. |
| W9 | **Config reloaded from disk per request** | Serve is keep's first long-lived process; per-request `Load()` keeps it semantically identical to a CLI invocation at that moment (D4 unthreatened). Invalid on-disk Config mid-edit → serve stays up, UI shows a "Config invalid" banner and degrades to read-only. No fsnotify, no cache. Being long-lived, serve also runs the D23 opportunistic log rotation on an hourly tick — a resident serve would otherwise starve rotation between CLI invocations. |
| W10 | **Live updates: poll status, SSE for logs** | Service list polls `GET /api/v1/services` (~3s, visible tab only — `Status()` is cheap at this service count). Log follow is SSE from a dedicated rotation-aware follower in serve (same copytruncate semantics as `keep logs -f`, but emitting structured per-stream events and sharing offsets with the backlog so no lines fall in the tail-to-follow gap). Verb responses return fresh status so the UI updates instantly. No WebSockets — nothing is bidirectional. |
| W11 | **Frontend: Vite+ SPA — React + TypeScript + Tailwind** | Lives in `web/`; `vp` (Vite+) drives dev/build/check. Built `dist` is gitignored and embedded via `go:embed`; PWA manifest + icons for home-screen install. The vite-plus toolchain packages are pinned to exact versions via the pnpm catalog + lockfile; the `vp` launcher itself (v0.1.x) floats with the curl installer — upgrades to the pinned toolchain are deliberate. |
| W12 | **Wiring: `127.0.0.1:4098`, Service `keep-web`** | `keep serve --host 127.0.0.1 --port 4098` (defaults), Config Service `keep-web` → label `keep.keep-web`. Tailscale exposure stays fully external per D12: `svc:keep` → `https://keep.<tailnet>.ts.net`. |
| W13 | **Build/release: frontend built in the release pipeline** | GoReleaser before-hook runs the vp build; release workflow installs the vp launcher first (toolchain versions come pinned from the lockfile). Local `go build` needs one frontend build (Make target) — acceptable since the install path is GitHub releases, not `go install` (D15). New PR CI covers both halves: go vet/test + vp check/build. Frontend unit tests are deferred: `vp test` is currently unusable upstream (v0.x `vite-plus-test` ships no vitest bin); revisit when Vite+'s test runner stabilizes. |

## Command surface

- `keep serve` — run the web UI + API in the foreground. Flags: `--host` (default `127.0.0.1`),
  `--port` (default `4098`). Declared in the Config as a resident Service like any other.

## API surface (v1, shape not final)

| Endpoint | Meaning |
|---|---|
| `GET  /api/v1/services` | status list (D24 `ServiceStatus` shapes) |
| `GET  /api/v1/services/{name}` | one service's status |
| `POST /api/v1/services/{name}/up\|down\|bounce` | verbs; response includes refreshed status; self-down refused |
| `GET  /api/v1/services/{name}/logs` | tail (last N lines, stdout/stderr) |
| `GET  /api/v1/services/{name}/logs/stream` | SSE follow (backlog + live, per-stream events) |
| `GET  /api/v1/services/{name}/show` | resolved command + env, secrets masked |
| `GET  /api/v1/diff` · `GET /api/v1/doctor` | read-only Plan / Findings |
| `GET  /api/v1/meta` | version, self service name, config path |
| `POST /api/v1/auth/login` · `POST /api/v1/auth/logout` | password session in / out (logout needs no auth but is origin-guarded) |
| `GET  /api/v1/auth/state` · `GET /api/v1/auth/me` | login-screen capabilities (unauthenticated) / session check |
| `POST /api/v1/auth/passkeys/register\|login/begin\|finish` | WebAuthn ceremonies |
| `GET/DELETE /api/v1/auth/passkeys[/{id}]` | passkey management (list, revoke) |

Auth: session cookie (browser) or `Authorization: Bearer` (scripting) on everything except the
login/auth-state endpoints and static assets.

## Accepted risks / tradeoffs

- **Vite+ maturity** — v0.x toolchain in the release path; toolchain packages pinned, upgraded
  deliberately. Already bit once: `vp test` is unusable upstream, so frontend unit tests are
  deferred (W13).
- **Passkeys are origin-bound** — registered against the ts.net hostname; renaming the Tailscale
  Service means re-registering. Loopback access falls back to password.
- **No cross-process locking** — concurrent verbs from serve + CLI race exactly like two CLI
  invocations today; launchctl operations are idempotent enough.
- **Rollout gotcha** — advertising a new Tailscale Service resets approval for *all* services on
  the node; plan to re-approve everything in the admin console when `svc:keep` lands.
- **Loopback is open** — any process running as the user can hit `:4098`… and can already run
  the keep CLI directly; app auth is a speed bump locally, a real gate on the tailnet.

## Deferred (post-v1)

- **Apply/diff-and-apply from the browser** — revisit only if phone-side config workflows appear.
- **Config editing in the UI** — v3 at best.
- **UI-minted, individually revocable API tokens** — v1 uses one static token from an env_file.
- **Multi-machine** — serve manages the Mac it runs on; a fleet view is out of scope.

## Non-goals

- **Replacing the CLI/TUI.** The web UI is a third shell over the same Manager, not the primary
  interface.
- **Managing Tailscale exposure.** D12 stands: keep runs `keep serve` as a Service; the
  `svc:keep` VIP, serve config, and grants remain external and separately coordinated.
