import { useCallback, useEffect, useState } from "react";
import { startRegistration } from "@simplewebauthn/browser";
import { api, type Passkey } from "../api";
import { useAuth } from "../auth";
import { ConfirmSheet } from "../components/ConfirmSheet";
import { useMeta } from "../hooks/useMeta";

export function SettingsPage() {
  const { logout } = useAuth();
  const meta = useMeta();
  const [passkeys, setPasskeys] = useState<Passkey[]>([]);
  const [message, setMessage] = useState<string | null>(null);
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState("iPhone");
  const [busy, setBusy] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Passkey | null>(null);

  const reload = useCallback(() => {
    api
      .passkeys()
      .then((r) => setPasskeys(r.passkeys))
      .catch(() => setPasskeys([]));
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  const addPasskey = async () => {
    setBusy(true);
    setMessage(null);
    try {
      const begin = await api.passkeyRegisterBegin();
      const credential = await startRegistration({
        optionsJSON: begin.options.publicKey as Parameters<
          typeof startRegistration
        >[0]["optionsJSON"],
      });
      await api.passkeyRegisterFinish(begin.ceremony_id, name.trim() || "passkey", credential);
      setMessage(`Passkey "${name.trim() || "passkey"}" registered.`);
      setNaming(false);
      reload();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const removePasskey = async (pk: Passkey) => {
    setBusy(true);
    try {
      await api.deletePasskey(pk.id);
      setPendingDelete(null);
      reload();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
      setPendingDelete(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="px-4 pt-3">
      <h2 className="mb-2 text-base font-bold">Settings</h2>

      <section className="rounded-xl border border-edge bg-panel px-4 py-3">
        <h3 className="text-sm font-semibold">Passkeys</h3>
        <p className="mt-0.5 text-xs text-dim">
          Passkeys are bound to this hostname; register one per device for Face ID sign-in.
        </p>
        <ul className="mt-2 flex flex-col gap-2">
          {passkeys.map((pk) => (
            <li key={pk.id} className="flex items-center justify-between gap-2 text-sm">
              <div className="min-w-0">
                <p className="truncate font-medium">{pk.name}</p>
                <p className="text-xs text-dim">
                  added {new Date(pk.created_at).toLocaleDateString()}
                  {pk.last_used_at &&
                    ` · last used ${new Date(pk.last_used_at).toLocaleDateString()}`}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setPendingDelete(pk)}
                className="rounded-md border border-red-500/50 px-2.5 py-1.5 text-xs font-medium text-red-400"
              >
                Delete
              </button>
            </li>
          ))}
          {passkeys.length === 0 && <li className="text-xs text-dim">No passkeys registered.</li>}
        </ul>

        {naming ? (
          <div className="mt-3 flex gap-2">
            {/* text-base (16px) so iOS Safari doesn't zoom on focus */}
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Passkey name"
              autoFocus
              className="min-w-0 flex-1 rounded-lg border border-edge bg-bg px-3 py-2.5 text-base outline-none focus:border-emerald-500"
            />
            <button
              type="button"
              onClick={() => void addPasskey()}
              disabled={busy}
              className="rounded-lg bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white disabled:opacity-50"
            >
              {busy ? "…" : "Create"}
            </button>
            <button
              type="button"
              onClick={() => setNaming(false)}
              disabled={busy}
              className="rounded-lg border border-edge px-3 py-2.5 text-sm font-medium"
            >
              Cancel
            </button>
          </div>
        ) : (
          <button
            type="button"
            onClick={() => setNaming(true)}
            className="mt-3 w-full rounded-lg bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white"
          >
            Register a passkey on this device
          </button>
        )}
        {message && <p className="mt-2 text-xs text-dim">{message}</p>}
      </section>

      <section className="mt-3 rounded-xl border border-edge bg-panel px-4 py-3 text-sm">
        <h3 className="font-semibold">About</h3>
        <dl className="mt-1.5 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
          <dt className="text-dim">version</dt>
          <dd className="font-mono">{meta?.version || "?"}</dd>
          <dt className="text-dim">config</dt>
          <dd className="break-all font-mono">{meta?.config_path || "?"}</dd>
          {meta?.self_service && (
            <>
              <dt className="text-dim">running as</dt>
              <dd className="font-mono">{meta.self_service}</dd>
            </>
          )}
        </dl>
      </section>

      <button
        type="button"
        onClick={() => void logout()}
        className="mt-4 w-full rounded-lg border border-edge px-4 py-2.5 text-sm font-semibold text-dim"
      >
        Sign out
      </button>

      {pendingDelete && (
        <ConfirmSheet
          title={`Delete passkey "${pendingDelete.name}"`}
          message="This device will need the password (or another passkey) to sign in again."
          confirmLabel="Delete"
          danger
          busy={busy}
          onConfirm={() => void removePasskey(pendingDelete)}
          onCancel={() => setPendingDelete(null)}
        />
      )}
    </div>
  );
}
