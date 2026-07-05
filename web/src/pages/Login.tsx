import { useEffect, useState, type FormEvent } from "react";
import { api, type AuthState } from "../api";
import { useAuth } from "../auth";

export function LoginPage() {
  const { login, passkeyLogin } = useAuth();
  const [authState, setAuthState] = useState<AuthState | null>(null);
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .authState()
      .then(setAuthState)
      .catch(() => setAuthState({ password_enabled: true, has_passkeys: false }));
  }, []);

  const submitPassword = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(password);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const signInWithPasskey = async () => {
    setBusy(true);
    setError(null);
    try {
      await passkeyLogin();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="safe-top mx-auto flex min-h-dvh max-w-sm flex-col justify-center px-6">
      <div className="mb-8 flex items-center gap-2.5">
        <span className="inline-block h-3 w-3 rounded-full bg-emerald-400" />
        <h1 className="text-2xl font-bold tracking-tight">keep</h1>
      </div>

      {authState?.has_passkeys && (
        <button
          type="button"
          onClick={() => void signInWithPasskey()}
          disabled={busy}
          className="mb-4 w-full rounded-lg bg-emerald-600 px-4 py-3 text-sm font-semibold text-white disabled:opacity-50"
        >
          Sign in with passkey
        </button>
      )}

      {authState?.password_enabled !== false && (
        <form onSubmit={(e) => void submitPassword(e)} className="flex flex-col gap-3">
          {/* text-base (16px): anything smaller makes iOS Safari zoom the page on focus */}
          <input
            type="password"
            autoComplete="current-password"
            placeholder="Password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full rounded-lg border border-edge bg-panel px-4 py-3 text-base outline-none focus:border-emerald-500"
          />
          <button
            type="submit"
            disabled={busy || password === ""}
            className="w-full rounded-lg border border-edge bg-panel px-4 py-3 text-sm font-semibold disabled:opacity-50"
          >
            Sign in with password
          </button>
        </form>
      )}

      {error && <p className="mt-4 text-sm text-red-400">{error}</p>}
    </div>
  );
}
