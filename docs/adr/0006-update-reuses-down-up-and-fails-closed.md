# `update` reuses Down/Up and fails closed as a Hold

**Status:** accepted

## Context

An Update replaces the software behind a Service while launchd would otherwise keep that
Service alive: a `KeepAlive`'d resident must not be running — or resurrectable — while its
binary is swapped underneath it. A lighter stop (bootout without disable) leaves a window where
a concurrent `apply` or a reboot brings the Service back mid-run. And when an update command
fails, the Service is stopped with its software in an unknown state; something has to decide
what state it is left in, and how that state is surfaced.

## Decision

`keep update <service>` is composed entirely from the existing verbs and states — it adds no
new lifecycle state:

- Take a per-service exclusive lock (flock) for the run's duration. A held lock refuses a
  second run fast, and a non-blocking probe of the same lock is what `status` reports as
  `updating`.
- **Down** the Service — the same persistent disable + bootout as the verb (ADR-0003), so
  `apply` and reboots cannot resurrect it mid-update.
- Run the declared update commands sequentially, stopping at the first non-zero exit, bounded
  by a whole-run timeout.
- On success, restore the *prior* state: **Up** only if the Service was not already held before
  the run. A pre-existing Hold is respected — Update never undoes a deliberate `down`.
- On any failure (command exit, timeout, Up error), the Service stays Down — an ordinary
  **Hold**, surfaced by `status`/`diff` like any other.

## Why

Fail closed with zero new state. "Stopped after a failed update" is exactly a Hold, which the
existing drift surfaces already report loudly; a parallel half-stopped state would need its own
handling in `apply`, `diff`, `status`, and the UI. Reusing Down/Up also carries over the tested
idempotent launchctl handling instead of a second stop/start code path.

## Consequences

- A failed update leaves the Service persistently down until `keep up` or a successful retry.
  Deliberate: availability is worth less than running half-updated software.
- A failed-update Hold is indistinguishable from a deliberate `down` in `status` alone; the
  update log (`<name>.update.log`) is the record of what happened.
- Because the run's first act Downs its target, the serve API must refuse Update on the Service
  serving the request — the Down would kill the updater mid-run and the Service would never
  come back Up (same family as the W3 self-down block). Updating the serve Service is done from
  the CLI, a separate process.
- A run started by `keep serve` lives in the serve process; if serve dies mid-run, the target
  is left held Down (the flock self-releases, so `updating` clears). Accepted for v1 over a
  detached survivor process, which is a mini init-system.
