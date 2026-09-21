# Runtime adapters contribute their own doctor findings

**Status:** accepted

## Context

The systemd adapter brings health checks that mean nothing on macOS: whether the user manager is
reachable at all, whether lingering is on (without it every Service dies at logout), and whether
systemd is new enough for `StandardOutput=append:`. `keep doctor` lives in `internal/keep`, which
by ADR-0008 must not import an adapter, so it cannot ask systemd these questions directly. A
`GOOS` switch inside doctor would put the runtime knowledge back above the seam that ADR-0008 just
removed it from.

## Decision

`internal/runtime` gains an optional capability:

```go
type Diagnosis struct { Severity Severity; Problem, Fix string }
type Diagnoser interface { Diagnose() []Diagnosis }
```

Doctor type-asserts its `Runtime` to `Diagnoser`, maps whatever comes back onto its existing
`Finding`, and reports those first, because a broken runtime environment explains every
per-Service symptom under it. An adapter with nothing OS-specific to check implements nothing and
contributes nothing; launchd does not implement it today.

The capability is optional rather than part of `Runtime` because most of `Runtime` is machinery
every adapter must have, and this is not. Forcing a `Diagnose()` on launchd would mean a method
that returns nil forever.

Diagnoses are environment-level and carry no Service name. `keep doctor` prints those without the
`<service>:` prefix rather than printing a placeholder that reads like a Service named `-`.

## Consequences

Doctor gained a second source of findings, and the JSON shape did not change: a runtime diagnosis
marshals as a `Finding` with `service` omitted, which the web UI already renders.

An adapter can now report a problem it cannot fix, which is the point: keep never runs
`loginctl enable-linger` for the user. It says what is wrong and what command fixes it (D13).

The reachability check is duplicated: `detectRuntime()` probes the systemd user manager before
returning the adapter, and `Diagnose()` checks it again. That is deliberate. The first stops keep
from pretending it can drive a runtime it cannot reach, and the second catches a session that went
away after the Manager was built.
