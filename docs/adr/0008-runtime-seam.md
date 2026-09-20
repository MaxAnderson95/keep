# The OS service runtime sits behind one seam, with an adapter per runtime

**Status:** accepted

## Context

keep is launchd-only. Running it on Linux means driving `systemd --user` instead, and the cheap
way to get there — `if GOOS == "linux"` wherever launchd appears — would spread runtime knowledge
across the orchestration layer and make every later change twice as expensive.

`internal/launchd.Controller` was already an interface with two adapters (the launchctl-backed one
and an in-memory fake), and `GOOS=linux go build` already succeeded. What blocked a second runtime
was launchd knowledge above that interface:

- one artifact per Service at `<dir>/<label>.plist`, assumed by the artifact path helper, the
  prune step, and the scan's extension filter. systemd needs a `.service` and, for a scheduled
  Service, a `.timer`.
- `launchd.Job`, `launchd.Render`, and `launchd.ReadMarkers` called directly from the Manager,
  the scan, and doctor.
- `PrintInfo.State` carried a raw launchctl string that keep compared against `"running"`.
- the verbs were launchd mechanisms rather than intentions: `Bootstrap(plistPath)` takes a path
  that only launchd's loader understands, and prune called `Enable` to clear launchd's disable
  record, which is the inverse of what systemd needs.

## Decision

One seam, `internal/runtime`, holds the OS-neutral vocabulary: `Job`, `Artifact`, `Target`,
`State`, `Info`, `MarkerInfo`, and the `Runtime` interface. Each OS gets one adapter behind it.

- **Adapters own rendering, control, and observation. The Manager owns every byte written to
  disk.** An adapter returns `[]Artifact{Path, Data}`; the Manager decides whether the bytes
  changed, writes them, and deletes them on prune. Nothing else in keep can write an artifact, and
  no adapter can.
- **Verbs name intent, not mechanism.** `Load`, `Unload`, `Hold`, `Release`, `Start`, `Restart`,
  `Forget`. `Hold` and `Release` are a pair because that is the durable-disable behaviour keep
  promises in ADR-0003, whatever the runtime calls it. `Forget` exists separately from `Unload`
  because pruning has to clear persistent records an unload leaves behind.
- **`State` is an enum with a distinct `Unknown`.** "The runtime has never heard of this label" is
  not "stopped": the first is drift keep should fix, the second is a Service sitting idle.
  `LastExit` is a pointer and is carried independently of `State`, so a scheduled Service that
  failed still reports its exit code once the next run is live.
- **`Target` carries `Scheduled`.** systemd controls a scheduled Service through its `.timer`
  rather than its `.service`; passing that fact to the verb lets the adapter route without
  probing disk for what it emitted.
- **`Forget` takes a bare label** because a pruned Service is gone from the Config, so keep cannot
  say whether it was scheduled. The adapter tries every unit form it could have emitted.
- **A Service rolls up its artifacts.** Plan reports one entry per Service whatever the count:
  `add` if any file is missing, `update` if any differs, naming the files in the reason. Prune
  deletes a Service's files as a group, and deletes them *before* `Forget`, so a runtime that
  re-reads disk while forgetting a unit does not find them still there.
- **Adapter selection lives in `internal/keep`, not in `internal/runtime`.** Adapters import the
  seam for its types, so a `runtime.Detect()` that imported the adapters would be an import cycle.
  `detectRuntime()` switches on `GOOS` and is the only place in the orchestration layer that names
  a concrete runtime.
- **launchd's output is byte-identical to what keep rendered before the seam existed**, locked by
  golden files generated from the pre-refactor renderer.

## Consequences

The Linux adapter is now additive: a new package plus one branch in `detectRuntime()`. The seam
has two real adapters today — launchctl and the in-memory fake keep's own tests drive — so it is a
seam rather than speculative indirection, and the fake deliberately renders a non-plist format so
nothing above the seam can quietly re-learn launchd's file layout.

`Loaded` stopped being a keep concept. The `loaded` field in the status JSON is now
`State != Unknown`, and doctor says "not loaded in the runtime" rather than "in launchd".

The byte-identical requirement is not cosmetic. `keep serve` is itself a Service under keep, so an
artifact change on the first upgraded apply would reload the process running the apply.

Rendering into a directory the adapter chooses means the Manager no longer overrides where
artifacts go; tests point the fake adapter at a temp directory instead. One source of truth for
the artifact directory, rather than a Manager-side override that could disagree with the adapter
that loads the file.
