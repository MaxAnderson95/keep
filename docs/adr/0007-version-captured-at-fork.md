# Service version is captured at fork and displayed only for the live process

**Status:** accepted

## Context

After `keep update` runs, the obvious next question is whether it took: what version is this
Service actually running now? keep has no answer today, and the natural-looking one — run
`app --version` when something asks for status — answers a subtly different question. That
command executes the binary *on disk*. A resident Service updated at 02:00 and never bounced
would report the new version while its live process still executes the old code, which is
precisely the case worth catching.

Running it at read time is also expensive in exactly the wrong places. `status` is not a
one-shot human command: the TUI calls it every 2 seconds, the web UI polls four pages every 3
seconds, and `keep serve` builds a fresh orchestrator per request. An arbitrary per-service
subprocess on that path is a configurable health probe by another name — the thing D10 ruled
out.

## Decision

A Service may declare an optional `version_command`. keep runs it **once, inside `keep fork`,
immediately before `syscall.Exec`**, and caches the result. Every surface reads the cache; no
surface ever runs the command.

- **Fork is the capture point** because it is the only path every real start goes through:
  `keep up`, `bounce`, `update`, launchd's own `KeepAlive` respawns, and login-time bootstraps.
  `keep up`/`bounce` alone would miss the starts keep never initiated.
- **The captured value is the running version, not the installed one.** Fork keeps its PID
  across `exec`, so it stamps `os.Getpid()` into the cache entry and that is exactly the live
  process's PID. Cache-vs-live matching is therefore exact, not heuristic.
- **Capture can never break or meaningfully delay a start.** It runs synchronously with the
  Service's assembled env, working directory, and umask, under a fixed 5s timeout, with output
  captured to a buffer rather than the Service's logs. Every failure — non-zero exit,
  unresolvable binary, timeout, empty output — is recorded in the cache entry and swallowed;
  fork execs regardless.
- **Parsing is fixed, with no knob:** the first non-empty line, trimmed, capped at 200 bytes,
  preferring stdout and falling back to stderr. `git version 2.4.5` displays verbatim.
- **The cache is one file per Service** at `<state dir>/versions/<name>.json`, written
  temp-then-rename. At login launchd bootstraps every agent at once; per-service files mean
  concurrent forks never contend and never need a lock.
- **Display is strict.** A version is shown only when the Service is running, the entry's PID
  matches the live PID, *and* the entry's recorded command matches the current Config. Otherwise
  nothing is shown — there is no "last known" fallback.
- **`version_command` is rejected on scheduled Services** in validation, because a scheduled
  Service is idle by definition and the strict rule would silently display nothing forever.

`version_command` is **optional in the strongest sense**: a Config that declares none must
behave, byte for byte, as it did before this feature existed. Fork does no extra work — no
subprocess, no directory creation, no file write. No validation rule can fail because of it. No
finding is emitted for it. The `versions/` directory is never created. The VERSION column and
the web chip appear only when at least one displayed Service declares the command, so existing
output is unchanged. This is a normative requirement with tests, not a side effect.

## Why

Capturing at start collapses two problems into one solution. It makes the number mean "what
this live process is running" — the honest and more useful reading — and it removes the
subprocess from every read path, so a 2-second TUI poll costs a file read. The strict PID match
is what buys the honesty: without it the cache is just a stale string, and a stale version
string is worse than no version string, because it reads as current.

Fork is the only complete seam, and it is already the place where keep does per-start work
(env assembly, umask, chdir), so the capture sits with its peers rather than in a new mechanism.

## Consequences

- A crash-looping resident with a `version_command` pays up to 5s of capture per respawn.
  launchd throttles restarts to roughly 10s, so worst case it spends a third of its cycle
  capturing a version. Accepted: a crash-looping service has a bigger problem.
- Declaring or editing `version_command` does not change the generated plist, so `apply` sees
  no change and nothing restarts — the version stays absent until the next natural start.
  `keep doctor` reports this as an info finding rather than leaving a silent blank, and `apply`
  deletes any entry whose recorded command no longer matches the Config so a string produced by
  a command you have since edited is never displayed.
- All capture failures are invisible at fork time by design, so `keep doctor` is the only place
  a broken `version_command` surfaces: it both statically resolves the binary and reports the
  error recorded by the last start.
- This is keep's first configurable subprocess, which does widen the D10 line. It is justified
  by running once per start rather than once per read, but it is a precedent, and the same
  argument should not be stretched to cover a probe that runs at read time.
- Scheduled Services cannot report a version at all. If that becomes painful, the fix is a
  deliberate second rule (show the last fire's capture), not a loosening of the strict match.
