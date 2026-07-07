import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { startAuthentication } from "@simplewebauthn/browser";
import { api, ApiError } from "./api";

type AuthPhase = "loading" | "authed" | "anon";

interface AuthContextValue {
  phase: AuthPhase;
  login: (password: string) => Promise<void>;
  passkeyLogin: () => Promise<void>;
  logout: () => Promise<void>;
  /** Called by data hooks when the API answers 401 (expired session). */
  invalidate: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [phase, setPhase] = useState<AuthPhase>("loading");

  useEffect(() => {
    let cancelled = false;
    api
      .me()
      .then(() => {
        if (!cancelled) setPhase("authed");
      })
      .catch(() => {
        if (!cancelled) setPhase("anon");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const login = useCallback(async (password: string) => {
    await api.login(password);
    setPhase("authed");
  }, []);

  const passkeyLogin = useCallback(async () => {
    const begin = await api.passkeyLoginBegin();
    const credential = await startAuthentication({
      // The server derives these options from the request origin; the cast
      // bridges go-webauthn's JSON to SimpleWebAuthn's identical shape.
      optionsJSON: begin.options.publicKey as Parameters<
        typeof startAuthentication
      >[0]["optionsJSON"],
    });
    await api.passkeyLoginFinish(begin.ceremony_id, credential);
    setPhase("authed");
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setPhase("anon");
    }
  }, []);

  const invalidate = useCallback(() => setPhase("anon"), []);

  const value = useMemo(
    () => ({ phase, login, passkeyLogin, logout, invalidate }),
    [phase, login, passkeyLogin, logout, invalidate],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth outside AuthProvider");
  return ctx;
}

/** True when an error means the session is gone and login is required. */
export function isAuthError(err: unknown): boolean {
  return err instanceof ApiError && err.status === 401;
}
