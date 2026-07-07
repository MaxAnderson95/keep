# Serve requires app-level auth (password + passkeys + bearer token) despite tailnet gating

**Status:** accepted

## Context

`keep serve` binds loopback and is exposed through the established Tailscale Service pattern
(userspace tailscaled proxying `svc:keep` → `127.0.0.1:4098`), so tailnet grants already gate who
can reach it. Arguments existed for shipping no auth at all: local processes running as the user
can invoke the keep CLI directly, so loopback auth adds little against local threats, and
Tailscale identity headers are spoofable from localhost. But the browser changes the threat
model — any web page open on any tailnet device can fire cross-origin requests at the ts.net
hostname (CSRF), and a management surface that can stop services warrants defense in depth. The
precedent is the OpenChamber deployment, which runs a UI password (`OPENCHAMBER_UI_PASSWORD` via
an env_file) behind the same tailnet gating.

## Decision

Serve authenticates at the application level:

1. **Password** — `KEEP_SERVE_PASSWORD`, supplied through the existing `env_files` mechanism
   (D6/D22); never in the Config or plist.
2. **Passkeys (WebAuthn)** — registered from the UI after password login (Face ID thereafter),
   implemented with a library (go-webauthn), credentials origin-bound to the ts.net hostname.
3. **Bearer token** — `KEEP_SERVE_TOKEN` for non-browser consumers (curl, iOS Shortcuts) of the
   first-class `/api/v1`.

Sessions are stateless signed cookies (~30-day expiry, `SameSite=Strict`) signed with a
persistent key, so bouncing serve does not log devices out. Passkey credentials and the signing
key live in a state file under `~/Library/Application Support/keep/` — outside `~/.config/keep`,
so machine-local state never tangles with the dotfiles-managed Config. Mutating endpoints
additionally enforce Origin/`Sec-Fetch-Site` checks; bearer-token requests carry no cookies and
are exempt from the CSRF checks.

## Why

- Tailnet grants are the outer gate; app auth is the inner one. Either can fail (mis-scoped
  grant, borrowed device, browser-borne CSRF) without the other failing with it.
- Passkeys make the inner gate nearly free day-to-day: one password entry per device, Face ID
  after.
- A static env-file token keeps the scriptable API usable without inventing token-management UI
  in v1.

## Consequences

- keep grows its **first machine-local state file** beyond logs and generated plists (passkey
  credentials + signing key). It is not portable and must never be committed.
- Passkeys are origin-bound: renaming the Tailscale Service hostname means re-registering;
  loopback access falls back to password.
- Session revocation is decoupled from password rotation: sessions are signed with the state
  file's key, not the password, so rotating a leaked `KEEP_SERVE_PASSWORD` does not invalidate
  already-issued sessions (up to their ~30-day expiry). Today the only revocation lever is
  deleting the state file — which also destroys registered passkeys. If this ever matters in
  practice, the natural fix is a `keep serve` maintenance flag that rotates only the signing
  key.
- Serve is inert until its secrets exist: the `keep-web` Service entry must reference an
  env_file providing `KEEP_SERVE_PASSWORD` and/or `KEEP_SERVE_TOKEN` — serve refuses to start
  with neither. Token-only is allowed (API-only deployments) but disables browser login, so no
  passkey can ever be bootstrapped that way.
- Local processes bypassing the UI entirely remain out of scope — they can already run the CLI;
  the app layer is a speed bump locally and a real gate on the tailnet.
