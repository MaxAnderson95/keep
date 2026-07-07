# Keep

The shared language of `keep` — a rootless, launchd-native CLI for declaring and managing
long-running background services on macOS. This file is a glossary only: it defines what our
terms *mean*, never how they're implemented.

## Language

**Service**:
A background process you declare in keep's config and that keep manages end-to-end —
generate, install, and control. The single unit of everything keep does.
_Avoid_: daemon, job, agent (those name the launchd mechanism, not your declared intent).

**Managed**:
The scope boundary. A launchd job is *managed* only if keep created and owns it; everything
else on the machine (Apple, vendor, hand-made agents) is *unmanaged* and keep never touches it.
_Avoid_: owned, controlled.

**Resident service**:
A Service that runs continuously; launchd keeps it alive, and its healthy state is "running".
The opencode/openchamber/t3/tailscaled servers are all resident.
_Avoid_: daemon, server, long-running.

**Scheduled service**:
A Service that runs on a calendar time or interval and then exits; its healthy state is
"idle, waiting for the next fire" — not "running". The opencode-db-backup job is scheduled.
_Avoid_: cron, periodic, timer.

**Config**:
The single declarative file that lists every Service and is the sole source of truth.
Hand-edited and version-controlled in `~/dotfiles`.
_Avoid_: manifest, spec.

**Apply**:
The operation that makes the live system match the Config — keep (re)generates artifacts and
(re)loads the affected launchd agents.
_Avoid_: sync, deploy, install.

**Generated artifact**:
A launchd plist keep renders from the Config into `~/Library/LaunchAgents`.
Disposable, never hand-edited, never committed.
_Avoid_: output, build product.

**Fork**:
The hidden `keep fork <service>` launcher that launchd invokes to start a Service. It assembles
the environment, sets umask and working directory, then execs the real command, replacing itself.
Not for human use.
_Avoid_: run, exec, launch.

**Env file**:
A dotenv-format file (`KEY=value` / `export KEY=value`, with comments and quotes) listed in the
Config that keep parses and loads into a Service's environment at fork time. Not committed; may
hold secrets. Plain assignments only — no shell logic.
_Avoid_: secrets file.

**Up**:
The imperative action that enables and starts a Service (`launchctl enable` + `bootstrap`,
`kickstart` if needed).
_Avoid_: start, load.

**Down**:
The imperative action that persistently stops a Service (`launchctl disable` + `bootout`). It
stays down across reboot and `apply` until brought Up.
_Avoid_: stop, kill, unload.

**Bounce**:
The imperative action that restarts a running Service in place (`kickstart -k`).
_Avoid_: restart, reload.

**Update**:
The imperative action that refreshes the software behind a Service: Down, run the Service's
declared update commands (the `update:` list in the Config) in order, then return to the prior
state — Up again only if the Service wasn't held and every command succeeded. Any failure
leaves the Service in a Hold.
_Avoid_: upgrade (the underlying tools' word, not keep's).

**Hold**:
The state of a Service that is declared enabled but has been Down'd — launchd's disable database
is holding it stopped. Surfaced by `diff`/`status` as intentional drift.
_Avoid_: paused, suspended.

**Drift**:
Any divergence between the declared Config and the live launchd state — a held-down Service, an
unapplied change, or a hand-edited/missing generated plist. Surfaced by `diff` and `status`.
_Avoid_: skew, delta.
