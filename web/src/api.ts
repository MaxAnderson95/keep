// Typed client for keep serve's /api/v1 (W8). Shapes mirror the Go structs'
// JSON tags exactly; see internal/keep (ServiceStatus, Plan, Finding,
// Resolved) and internal/serve.

export type Health =
  | "running"
  | "idle"
  | "held"
  | "declared-off"
  | "stopped"
  | "not-loaded"
  | "error"
  | "updating";

export interface ServiceStatus {
  name: string;
  label: string;
  type: string;
  enabled: boolean;
  health: Health;
  loaded: boolean;
  pid?: number;
  uptime?: string;
  last_exit?: number;
  held: boolean;
  declared_off: boolean;
  drift: boolean;
  port?: number;
  port_listening?: boolean;
  has_update: boolean;
  updating: boolean;
}

export interface ServicePlan {
  name: string;
  label: string;
  kind: "add" | "update" | "noop" | "remove";
  held: boolean;
  declared_off: boolean;
  disabled_drift: boolean;
  reason?: string;
}

export interface Plan {
  services: ServicePlan[] | null;
  removes: ServicePlan[] | null;
}

export interface Finding {
  service?: string;
  severity: "error" | "warning";
  problem: string;
  fix: string;
}

export interface ShownEnv {
  key: string;
  value: string;
  source: string;
  secret: boolean;
}

export interface Resolved {
  name: string;
  type: string;
  label: string;
  argv: string[];
  working_dir?: string;
  umask?: string;
  env: ShownEnv[];
  update?: string[];
  update_timeout?: string;
}

export interface Meta {
  version: string;
  commit: string;
  self_service: string;
  config_path: string;
}

export interface AuthState {
  password_enabled: boolean;
  has_passkeys: boolean;
}

export interface Passkey {
  id: string;
  name: string;
  created_at: string;
  last_used_at?: string;
}

export type Verb = "up" | "down" | "bounce";

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  if (!res.ok) {
    let code = "http_error";
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = (await res.json()) as { error?: { code: string; message: string } };
      if (body.error) {
        code = body.error.code;
        message = body.error.message;
      }
    } catch {
      // non-JSON error body; keep the status text
    }
    throw new ApiError(res.status, code, message);
  }
  return (await res.json()) as T;
}

function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: "POST",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

export const api = {
  authState: () => request<AuthState>("/api/v1/auth/state"),
  me: () => request<{ authenticated: boolean; method: string }>("/api/v1/auth/me"),
  login: (password: string) => post<{ ok: boolean }>("/api/v1/auth/login", { password }),
  logout: () => post<{ ok: boolean }>("/api/v1/auth/logout"),

  meta: () => request<Meta>("/api/v1/meta"),
  services: () => request<{ services: ServiceStatus[] }>("/api/v1/services"),
  service: (name: string) => request<ServiceStatus>(`/api/v1/services/${encodeURIComponent(name)}`),
  verb: (name: string, verb: Verb) =>
    post<{ ok: boolean; status: ServiceStatus }>(
      `/api/v1/services/${encodeURIComponent(name)}/${verb}`,
    ),
  logs: (name: string, lines = 200) =>
    request<{ out: string[]; err: string[]; update?: string[] }>(
      `/api/v1/services/${encodeURIComponent(name)}/logs?lines=${lines}`,
    ),
  show: (name: string) => request<Resolved>(`/api/v1/services/${encodeURIComponent(name)}/show`),
  diff: () => request<Plan>("/api/v1/diff"),
  doctor: () => request<{ findings: Finding[] }>("/api/v1/doctor"),

  passkeys: () => request<{ passkeys: Passkey[] }>("/api/v1/auth/passkeys"),
  deletePasskey: (id: string) =>
    request<{ ok: boolean }>(`/api/v1/auth/passkeys/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  passkeyRegisterBegin: () =>
    post<{ ceremony_id: string; options: { publicKey: unknown } }>(
      "/api/v1/auth/passkeys/register/begin",
    ),
  passkeyRegisterFinish: (ceremonyId: string, name: string, credential: unknown) =>
    post<{ ok: boolean }>("/api/v1/auth/passkeys/register/finish", {
      ceremony_id: ceremonyId,
      name,
      credential,
    }),
  passkeyLoginBegin: () =>
    post<{ ceremony_id: string; options: { publicKey: unknown } }>(
      "/api/v1/auth/passkeys/login/begin",
    ),
  passkeyLoginFinish: (ceremonyId: string, credential: unknown) =>
    post<{ ok: boolean }>("/api/v1/auth/passkeys/login/finish", {
      ceremony_id: ceremonyId,
      credential,
    }),
};

export function logStreamUrl(name: string, lines = 200): string {
  return `/api/v1/services/${encodeURIComponent(name)}/logs/stream?lines=${lines}`;
}

// UpdateDone is the terminal SSE event of an update run (mirrors the Go
// updateDoneEvent: keep.UpdateResult + done flag + refreshed status).
export interface UpdateDone {
  done: boolean;
  service: string;
  ok: boolean;
  stayed_held: boolean;
  timed_out?: boolean;
  error?: string;
  status?: ServiceStatus;
}

// runUpdate POSTs .../update and consumes the SSE response with fetch
// (EventSource cannot POST): output lines go to onLine as they stream, and
// the terminal result event resolves the promise. The run is detached on the
// server — aborting or losing this request does NOT cancel the update.
export async function runUpdate(name: string, onLine: (line: string) => void): Promise<UpdateDone> {
  const res = await fetch(`/api/v1/services/${encodeURIComponent(name)}/update`, {
    method: "POST",
  });
  const contentType = res.headers.get("Content-Type") ?? "";
  if (!res.ok || !contentType.includes("text/event-stream")) {
    let code = "http_error";
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = (await res.json()) as { error?: { code: string; message: string } };
      if (body.error) {
        code = body.error.code;
        message = body.error.message;
      }
    } catch {
      // non-JSON error body; keep the status text
    }
    throw new ApiError(res.status, code, message);
  }
  if (!res.body) {
    throw new ApiError(0, "stream_unsupported", "response body is not streamable");
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  let done: UpdateDone | null = null;
  for (;;) {
    const { value, done: eof } = await reader.read();
    if (eof) break;
    buf += decoder.decode(value, { stream: true });
    for (;;) {
      const sep = buf.indexOf("\n\n");
      if (sep < 0) break;
      const frame = buf.slice(0, sep);
      buf = buf.slice(sep + 2);
      for (const line of frame.split("\n")) {
        if (!line.startsWith("data: ")) continue; // skip ping comments
        const ev = JSON.parse(line.slice(6)) as { line?: string } & Partial<UpdateDone>;
        if (ev.done) {
          done = ev as UpdateDone;
        } else if (ev.line !== undefined) {
          onLine(ev.line);
        }
      }
    }
  }
  if (!done) {
    throw new ApiError(
      0,
      "stream_interrupted",
      "the update stream ended without a result — the run continues on the server; check the update log",
    );
  }
  return done;
}
