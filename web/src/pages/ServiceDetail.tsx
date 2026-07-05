import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, logStreamUrl, type Resolved, type ServiceStatus, type Verb } from "../api";
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
          <VerbBar svc={svc} selfService={meta?.self_service ?? ""} onDone={refresh} />
          <ShowPanel name={name} />
          <LogViewer name={name} />
        </>
      )}
      {!svc && !error && <p className="px-4 py-8 text-center text-sm text-dim">Loading…</p>}
    </div>
  );
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

interface PendingVerb {
  verb: Verb;
  title: string;
  message: string;
  danger: boolean;
}

function VerbBar({
  svc,
  selfService,
  onDone,
}: {
  svc: ServiceStatus;
  selfService: string;
  onDone: () => void;
}) {
  const [pending, setPending] = useState<PendingVerb | null>(null);
  const [busy, setBusy] = useState(false);
  const [verbError, setVerbError] = useState<string | null>(null);

  const isSelf = selfService !== "" && svc.name === selfService;
  const isTailscaled = svc.name === "tailscaled";

  const confirmFor = (verb: Verb): PendingVerb => {
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
    return { verb, title: `${verb} ${svc.name}`, message, danger };
  };

  const run = async (verb: Verb) => {
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
      </div>
      {isSelf && (
        <p className="mt-1.5 text-xs text-dim">
          Down is disabled: this service runs the web UI you are using (use the CLI over SSH).
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
  stream: "out" | "err";
  line: string;
}

function LogViewer({ name }: { name: string }) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [filter, setFilter] = useState<"all" | "out" | "err">("all");
  const [paused, setPaused] = useState(false);
  const [connected, setConnected] = useState(false);
  const bottomRef = useRef<HTMLDivElement | null>(null);
  const stickRef = useRef(true);

  useEffect(() => {
    if (paused) return;
    setLines([]);
    const es = new EventSource(logStreamUrl(name));
    es.onopen = () => setConnected(true);
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
          {(["all", "out", "err"] as const).map((f) => (
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
            <span key={i} className={l.stream === "err" ? "text-rose-300" : ""}>
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
