import { api, type ServicePlan } from "../api";
import { ErrorBanner } from "../components/ErrorBanner";
import { usePoll } from "../hooks/usePoll";

const POLL_MS = 10000;

const KIND_STYLE: Record<ServicePlan["kind"], string> = {
  add: "text-emerald-300 border-emerald-500/40 bg-emerald-500/10",
  update: "text-amber-300 border-amber-500/40 bg-amber-500/10",
  noop: "text-dim border-edge bg-transparent",
  remove: "text-red-300 border-red-500/40 bg-red-500/10",
};

export function DiffPage() {
  const { data, error } = usePoll(() => api.diff(), POLL_MS);
  const services = data?.services ?? [];
  const removes = data?.removes ?? [];
  const rows = [...services, ...removes];
  const changes = rows.filter((r) => r.kind !== "noop" || r.held || r.disabled_drift);

  return (
    <div className="pt-3">
      <ErrorBanner error={error} />
      <div className="px-4">
        <h2 className="mb-2 text-base font-bold">Diff</h2>
        {data && changes.length === 0 && (
          <p className="rounded-xl border border-edge bg-panel px-4 py-6 text-center text-sm text-dim">
            In sync — apply would change nothing.
          </p>
        )}
        <ul className="flex flex-col gap-2">
          {rows.map((row) => (
            <li
              key={`${row.kind}-${row.name}`}
              className="rounded-xl border border-edge bg-panel px-4 py-3"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="truncate font-semibold">{row.name}</span>
                <span
                  className={`rounded-full border px-2 py-0.5 text-xs font-medium ${KIND_STYLE[row.kind]}`}
                >
                  {row.kind}
                </span>
              </div>
              {(row.held || row.declared_off || row.disabled_drift) && (
                <div className="mt-1 flex gap-2 text-xs">
                  {row.held && <span className="text-amber-400">held</span>}
                  {row.declared_off && <span className="text-dim">declared off</span>}
                  {row.disabled_drift && <span className="text-red-400">disabled drift</span>}
                </div>
              )}
              {row.reason && <p className="mt-1 text-xs text-dim">{row.reason}</p>}
            </li>
          ))}
        </ul>
        <p className="mt-3 text-center text-xs text-dim">
          Read-only — run <span className="font-mono">keep apply</span> from the Mac.
        </p>
      </div>
    </div>
  );
}
