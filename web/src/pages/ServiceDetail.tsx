import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  api,
  logStreamUrl,
  runUpdate,
  type Resolved,
  type ServiceStatus,
  type UpdateDone,
  type Verb,
} from "../api";
import { ConfirmSheet } from "../components/ConfirmSheet";
import { ErrorBanner } from "../components/ErrorBanner";
import { HealthBadge } from "../components/HealthBadge";
import { useMeta } from "../hooks/useMeta";
import { usePoll } from "../hooks/usePoll";

const POLL_MS = 3000;

export function ServiceDetailPage() {
  const { name = "" } = useParams();
  const { data: svc, error, refresh } = usePoll(() => api.service(name), POLL_MS);
  const meta = useMeta();
  const update = useUpdateRun(name, refresh);

  return (
    <div className="pt-3">
      <div className="mb-2 px-4">
        <Link to="/" className="text-sm text-dim">
          ← Services
        </Link>
      </div>
      <ErrorBanner error={error} />
      {svc && (
        <>
          <StatusPanel svc={svc} />
          <VerbBar
            svc={svc}
            selfService={meta?.self_service ?? ""}
            onDone={refresh}
            updateRunning={update.running}
            onUpdate={update.start}
          />
          <UpdateOutputPanel run={update} />
          <ShowPanel name={name} />
          <LogViewer name={name} hasUpdate={svc.has_update} />
        </>
      )}
      {!svc && !error && <p className="px-4 py-8 text-center text-sm text-dim">Loading…</p>}
    </div>
  );
}

interface UpdateRun {
  lines: string[];
  result: UpdateDone | null;
  error: string | null;
  running: boolean;
  started: boolean;
  start: () => void;
}

// useUpdateRun drives one update run's SSE stream. The run itself is
// detached on the server: losing this stream never cancels the update.
function useUpdateRun(name: string, onDone: () => void): UpdateRun {
  const [lines, setLines] = useState<string[]>([]);
  const [result, setResult] = useState<UpdateDone | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [started, setStarted] = useState(false);

  const start = () => {
    setLines([]);
    setResult(null);
    setError(null);
    setStarted(true);
    setRunning(true);
    void (async () => {
      try {
        const done = await runUpdate(name, (line) => setLines((prev) => [...prev, line]));
        setResult(done);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setRunning(false);
        onDone();
      }
    })();
  };

  return { lines, result, error, running, started, start };
}

function StatusPanel({ svc }: { svc: ServiceStatus }) {
  return (
    <section className="mx-4 rounded-xl border border-edge bg-panel px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <h2 className="truncate text-lg font-bold">{svc.name}</h2>
        <HealthBadge health={svc.health} pulse />
      </div>
      <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
        <Item label="type" value={svc.type} />
        <Item label="label" value={svc.label} mono />
        {svc.uptime && <Item label="uptime" value={svc.uptime} />}
        {svc.pid !== undefined && svc.pid > 0 && <Item label="pid" value={String(svc.pid)} mono />}
        {svc.last_exit !== undefined && (
          <Item label="last exit" value={String(svc.last_exit)} mono />
        )}
        {svc.port !== undefined && svc.port > 0 && (
          <Item
            label="port"
            value={`${svc.port}${svc.port_listening === true ? " (listening)" : svc.port_listening === false ? " (not listening)" : ""}`}
          />
        )}
        {svc.held && <Item label="hold" value="held down" />}
        {svc.drift && <Item label="drift" value="yes" />}
      </dl>
    </section>
  );
}

function Item({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs uppercase tracking-wide text-dim">{label}</dt>
      <dd className={`truncate ${mono ? "font-mono text-xs leading-5" : ""}`}>{value}</dd>
    </div>
  );
}

type ActionVerb = Verb | "update";

interface PendingVerb {
  verb: ActionVerb;
  title: string;
  message: string;
  danger: boolean;
}

function VerbBar({
  svc,
  selfService,
  onDone,
  updateRunning,
  onUpdate,
}: {
  svc: ServiceStatus;
  selfService: string;
  onDone: () => void;
  updateRunning: boolean;
  onUpdate: () => void;
}) {
  const [pending, setPending] = useState<PendingVerb | null>(null);
  const [busy, setBusy] = useState(false);
  const [verbError, setVerbError] = useState<string | null>(null);

  const isSelf = selfService !== "" && svc.name === selfService;
  const isTailscaled = svc.name === "tailscaled";

  const confirmFor = (verb: ActionVerb): PendingVerb => {
    let message = `Run ${verb} on ${svc.name}?`;
    let danger = false;
    if (verb === "down") {
      danger = true;
      message = `${svc.name} will be persistently held down — across reboots and apply — until brought back up.`;
      if (isTailscaled) {
        message +=
          "\n\nWARNING: tailscaled carries this connection. Downing it cuts off Tailscale access to this Mac, including this UI. Recovery requires SSH or the Mac itself.";
      }
    }
    if (verb === "bounce") {
      if (isSelf) {
        message =
          "This restarts the web UI you are using. The page will drop for a few seconds while launchd brings it back.";
      } else if (isTailscaled) {
        message =
          "tailscaled carries this connection — the UI may blip while it restarts. This is the usual fix for pending Tailscale services.";
      }
    }
    if (verb === "update") {
      message = `Down ${svc.name}, run its declared update commands, and bring it back up when they all succeed. The service is unavailable while the update runs; a failure leaves it held down.`;
      if (isTailscaled) {
        message +=
          "\n\nWARNING: tailscaled carries this connection. Tailscale access to this Mac — including this UI and this output stream — drops while it updates. The run continues on the server either way.";
        danger = true;
      }
    }
    return { verb, title: `${verb} ${svc.name}`, message, danger };
  };

  const run = async (verb: ActionVerb) => {
    if (verb === "update") {
      setPending(null);
      setVerbError(null);
      onUpdate();
      return;
    }
    setBusy(true);
    setVerbError(null);
    try {
      await api.verb(svc.name, verb);
      setPending(null);
      onDone();
    } catch (err) {
      setVerbError(err instanceof Error ? err.message : String(err));
      setPending(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="mx-4 mt-3">
      <div className="flex gap-2">
        <VerbButton label="Up" onClick={() => setPending(confirmFor("up"))} />
        <VerbButton label="Bounce" onClick={() => setPending(confirmFor("bounce"))} />
        <VerbButton
          label="Down"
          danger
          disabled={isSelf}
          onClick={() => setPending(confirmFor("down"))}
        />
        {svc.has_update && (
          <VerbButton
            label="Update"
            disabled={isSelf || svc.updating || updateRunning}
            onClick={() => setPending(confirmFor("update"))}
          />
        )}
      </div>
      {isSelf && (
        <p className="mt-1.5 text-xs text-dim">
          {svc.has_update ? "Down and Update are" : "Down is"} disabled: this service runs the web
          UI you are using (use the CLI over SSH).
        </p>
      )}
      {!isSelf && svc.updating && !updateRunning && (
        <p className="mt-1.5 text-xs text-dim">
          An update is already running (started elsewhere). Watch it via the update log stream
          below.
        </p>
      )}
      {verbError && <p className="mt-2 text-sm text-red-400">{verbError}</p>}
      {pending && (
        <ConfirmSheet
          title={pending.title}
          message={pending.message}
          confirmLabel={pending.verb}
          danger={pending.danger}
          busy={busy}
          onConfirm={() => void run(pending.verb)}
          onCancel={() => setPending(null)}
        />
      )}
    </section>
  );
}

// UpdateOutputPanel shows a triggered update run: live streamed output while
// it runs, then an explicit success / failed state (U12).
function UpdateOutputPanel({ run }: { run: UpdateRun }) {
  const bottomRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "instant", block: "end" });
  }, [run.lines]);

  if (!run.started) return null;

  let state: { label: string; cls: string };
  if (run.running) {
    state = { label: "running…", cls: "text-violet-300" };
  } else if (run.error) {
    state = { label: "failed", cls: "text-red-400" };
  } else if (run.result && run.result.ok) {
    state = {
      label: run.result.stayed_held ? "succeeded (stayed down)" : "succeeded",
      cls: "text-emerald-300",
    };
  } else {
    state = { label: run.result?.timed_out ? "timed out" : "failed", cls: "text-red-400" };
  }

  return (
    <section className="mx-4 mt-3 rounded-xl border border-edge bg-panel">
      <div className="flex items-center justify-between border-b border-edge px-4 py-2">
        <span className="text-sm font-semibold">Update</span>
        <span className={`text-xs font-medium ${state.cls}`}>{state.label}</span>
      </div>
      <div className="max-h-72 overflow-y-auto overscroll-contain px-3 py-2">
        {run.lines.length === 0 && (
          <p className="py-4 text-center text-xs text-dim">Waiting for output…</p>
        )}
        <pre className="whitespace-pre-wrap break-all font-mono text-[11px] leading-4">
          {run.lines.join("\n")}
        </pre>
        {run.error && <p className="mt-2 text-xs text-red-400">{run.error}</p>}
        {!run.running && run.result?.error && (
          <p className="mt-2 text-xs text-red-400">{run.result.error}</p>
        )}
        <div ref={bottomRef} />
      </div>
    </section>
  );
}

function VerbButton({
  label,
  danger,
  disabled,
  onClick,
}: {
  label: string;
  danger?: boolean;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={`flex-1 rounded-lg border px-4 py-2.5 text-sm font-semibold disabled:opacity-40 ${
        danger ? "border-red-500/50 text-red-400" : "border-edge bg-panel"
      }`}
    >
      {label}
    </button>
  );
}

function ShowPanel({ name }: { name: string }) {
  const [open, setOpen] = useState(false);
  const [resolved, setResolved] = useState<Resolved | null>(null);

  useEffect(() => {
    if (open && !resolved) {
      api
        .show(name)
        .then(setResolved)
        .catch(() => setResolved(null));
    }
  }, [open, resolved, name]);

  return (
    <section className="mx-4 mt-3 rounded-xl border border-edge bg-panel">
      <button
        type="button"
        className="flex w-full items-center justify-between px-4 py-3 text-sm font-semibold"
        onClick={() => setOpen((o) => !o)}
      >
        Resolved command &amp; env
        <span className="text-dim">{open ? "−" : "+"}</span>
      </button>
      {open && resolved && (
        <div className="border-t border-edge px-4 py-3 text-xs">
          <p className="break-all font-mono">{resolved.argv.join(" ")}</p>
          {resolved.working_dir && (
            <p className="mt-1 font-mono text-dim">cwd {resolved.working_dir}</p>
          )}
          {resolved.update?.map((u, i) => (
            <p key={i} className="mt-1 break-all font-mono text-dim">
              update[{i}] {u}
            </p>
          ))}
          <table className="mt-2 w-full">
            <tbody>
              {resolved.env.map((e) => (
                <tr key={e.key} className="align-top">
                  <td className="pr-2 font-mono text-dim">{e.key}</td>
                  <td className="break-all font-mono">{e.secret ? "········" : e.value}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

const MAX_LOG_LINES = 2000;

interface LogLine {
  stream: "out" | "err" | "update";
  line: string;
}

function LogViewer({ name, hasUpdate }: { name: string; hasUpdate: boolean }) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [filter, setFilter] = useState<"all" | "out" | "err" | "update">("all");
  const [paused, setPaused] = useState(false);
  const [connected, setConnected] = useState(false);
  const bottomRef = useRef<HTMLDivElement | null>(null);
  const stickRef = useRef(true);

  useEffect(() => {
    if (paused) return;
    setLines([]);
    const es = new EventSource(logStreamUrl(name));
    es.onopen = () => {
      setConnected(true);
      // EventSource auto-reconnects after a blip, and the server replays the
      // backlog on every new connection. Reset the buffer on (re)open so the
      // replay lands in an empty view instead of duplicating lines — it also
      // covers whatever was missed while disconnected.
      setLines([]);
    };
    es.onerror = () => setConnected(false);
    es.onmessage = (ev) => {
      const parsed = JSON.parse(ev.data) as LogLine;
      setLines((prev) => {
        const next = prev.length >= MAX_LOG_LINES ? prev.slice(-MAX_LOG_LINES + 1) : prev.slice();
        next.push(parsed);
        return next;
      });
    };
    return () => {
      es.close();
      setConnected(false);
    };
  }, [name, paused]);

  useEffect(() => {
    if (stickRef.current) bottomRef.current?.scrollIntoView({ behavior: "instant", block: "end" });
  }, [lines]);

  const visible = filter === "all" ? lines : lines.filter((l) => l.stream === filter);

  return (
    <section className="mx-4 mt-3 rounded-xl border border-edge bg-panel">
      <div className="flex items-center justify-between border-b border-edge px-3 py-2">
        <div className="flex gap-1">
          {(hasUpdate
            ? (["all", "out", "err", "update"] as const)
            : (["all", "out", "err"] as const)
          ).map((f) => (
            <button
              key={f}
              type="button"
              onClick={() => setFilter(f)}
              className={`rounded-md px-2.5 py-1 text-xs font-medium ${
                filter === f ? "bg-edge text-ink" : "text-dim"
              }`}
            >
              {f}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <span
            className={`inline-block h-1.5 w-1.5 rounded-full ${connected ? "bg-emerald-400" : "bg-zinc-600"}`}
          />
          <button
            type="button"
            onClick={() => setPaused((p) => !p)}
            className="rounded-md border border-edge px-2.5 py-1 text-xs font-medium"
          >
            {paused ? "Resume" : "Pause"}
          </button>
        </div>
      </div>
      <div
        className="h-72 overflow-y-auto overscroll-contain px-3 py-2"
        onScroll={(e) => {
          const el = e.currentTarget;
          stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
        }}
      >
        {visible.length === 0 && (
          <p className="py-6 text-center text-xs text-dim">No log output.</p>
        )}
        <pre className="whitespace-pre-wrap break-all font-mono text-[11px] leading-4">
          {visible.map((l, i) => (
            <span
              key={i}
              className={
                l.stream === "err"
                  ? "text-rose-300"
                  : l.stream === "update"
                    ? "text-violet-300"
                    : ""
              }
            >
              {l.line}
              {"\n"}
            </span>
          ))}
        </pre>
        <div ref={bottomRef} />
      </div>
    </section>
  );
}
