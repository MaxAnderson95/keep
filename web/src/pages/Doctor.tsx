import { api, type Finding } from "../api";
import { ErrorBanner } from "../components/ErrorBanner";
import { usePoll } from "../hooks/usePoll";

const POLL_MS = 10000;

// Severity sets the visual weight: red demands action, amber warns, and info
// stays neutral so an advisory finding never reads like a fault.
const SEVERITY: Record<Finding["severity"], { card: string; label: string }> = {
  error: { card: "border-red-500/40 bg-red-500/10", label: "text-red-300" },
  warning: { card: "border-amber-500/40 bg-amber-500/10", label: "text-amber-300" },
  info: { card: "border-edge bg-panel", label: "text-dim" },
};

export function DoctorPage() {
  const { data, error } = usePoll(() => api.doctor(), POLL_MS);

  return (
    <div className="pt-3">
      <ErrorBanner error={error} />
      <div className="px-4">
        <h2 className="mb-2 text-base font-bold">Doctor</h2>
        {data && data.findings.length === 0 && (
          <p className="rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-4 py-6 text-center text-sm text-emerald-200">
            Everything looks healthy.
          </p>
        )}
        <ul className="flex flex-col gap-2">
          {data?.findings.map((f, i) => (
            <li key={i} className={`rounded-xl border px-4 py-3 ${SEVERITY[f.severity].card}`}>
              <div className="flex items-center justify-between gap-2 text-sm">
                <span className="font-semibold">{f.service || "keep"}</span>
                <span className={`text-xs font-medium uppercase ${SEVERITY[f.severity].label}`}>
                  {f.severity}
                </span>
              </div>
              <p className="mt-1 text-sm">{f.problem}</p>
              <p className="mt-1 text-xs text-dim">{f.fix}</p>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
