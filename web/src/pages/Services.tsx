import { Link } from "react-router-dom";
import { api, type ServiceStatus } from "../api";
import { ErrorBanner } from "../components/ErrorBanner";
import { HealthBadge } from "../components/HealthBadge";
import { usePoll } from "../hooks/usePoll";

const POLL_MS = 3000;

export function ServicesPage() {
  const { data, error } = usePoll(() => api.services(), POLL_MS);

  return (
    <div className="pt-3">
      <ErrorBanner error={error} />
      {!data && !error && <p className="px-4 py-8 text-center text-sm text-dim">Loading…</p>}
      <ul className="flex flex-col gap-2 px-4">
        {data?.services.map((svc) => (
          <ServiceCard key={svc.name} svc={svc} />
        ))}
      </ul>
    </div>
  );
}

function ServiceCard({ svc }: { svc: ServiceStatus }) {
  return (
    <li>
      <Link
        to={`/services/${encodeURIComponent(svc.name)}`}
        className="block rounded-xl border border-edge bg-panel px-4 py-3 active:bg-edge/60"
      >
        <div className="flex items-center justify-between gap-2">
          <span className="truncate font-semibold">{svc.name}</span>
          <HealthBadge health={svc.health} pulse />
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-dim">
          <span>{svc.type}</span>
          {svc.uptime && <span>up {svc.uptime}</span>}
          {svc.pid !== undefined && svc.pid > 0 && <span>pid {svc.pid}</span>}
          {svc.port !== undefined && svc.port > 0 && (
            <span className={svc.port_listening === false ? "text-rose-400" : ""}>
              :{svc.port}
              {svc.port_listening === true && " ✓"}
              {svc.port_listening === false && " ✗"}
            </span>
          )}
          {svc.drift && <span className="font-medium text-amber-400">drift</span>}
        </div>
      </Link>
    </li>
  );
}
