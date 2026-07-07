import type { Health } from "../api";

const STYLES: Record<Health, { dot: string; text: string; label: string }> = {
  running: { dot: "bg-emerald-400", text: "text-emerald-300", label: "running" },
  idle: { dot: "bg-sky-400", text: "text-sky-300", label: "idle" },
  held: { dot: "bg-amber-400", text: "text-amber-300", label: "held" },
  "declared-off": { dot: "bg-zinc-500", text: "text-zinc-400", label: "declared off" },
  stopped: { dot: "bg-orange-400", text: "text-orange-300", label: "stopped" },
  "not-loaded": { dot: "bg-rose-400", text: "text-rose-300", label: "not loaded" },
  error: { dot: "bg-red-500", text: "text-red-400", label: "error" },
  updating: { dot: "bg-violet-400", text: "text-violet-300", label: "updating" },
};

export function HealthBadge({ health, pulse }: { health: Health; pulse?: boolean }) {
  const s = STYLES[health];
  return (
    <span className={`inline-flex items-center gap-1.5 text-sm font-medium ${s.text}`}>
      <span className="relative flex h-2.5 w-2.5">
        {pulse && (health === "running" || health === "updating") && (
          <span
            className={`absolute inline-flex h-full w-full animate-ping rounded-full opacity-40 ${s.dot}`}
          />
        )}
        <span className={`relative inline-flex h-2.5 w-2.5 rounded-full ${s.dot}`} />
      </span>
      {s.label}
    </span>
  );
}
