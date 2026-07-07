import { ApiError } from "../api";

/**
 * Error strip shown above stale-but-visible content. A 503 config_invalid
 * gets its dedicated wording (W9): serve is up, the on-disk Config is not.
 */
export function ErrorBanner({ error }: { error?: Error }) {
  if (!error) return null;
  const isConfig = error instanceof ApiError && error.code === "config_invalid";
  return (
    <div
      className={`mx-4 mb-3 rounded-lg border px-3 py-2 text-sm ${
        isConfig
          ? "border-amber-500/40 bg-amber-500/10 text-amber-200"
          : "border-red-500/40 bg-red-500/10 text-red-200"
      }`}
    >
      <p className="font-semibold">{isConfig ? "Config invalid" : "Request failed"}</p>
      <p className="mt-0.5 break-words font-mono text-xs opacity-80">{error.message}</p>
      {isConfig && (
        <p className="mt-1 text-xs opacity-70">
          Showing the last good view. Fix the Config on the Mac, then refresh.
        </p>
      )}
    </div>
  );
}
