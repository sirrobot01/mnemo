import type {
  CreateTaskRequest,
  DbStatus,
  Health,
  IngestResult,
  RenderedResume,
  ResumeRequest,
  SessionEvent,
  Task,
  TaskAction,
  WorkingState,
  AuthLoginResponse,
  AuthRequest,
} from "./types";

let authToken = "";

export class ApiError extends Error {
  status: number;

  constructor(status: number, body: string) {
    super(`${status}: ${body}`);
    this.status = status;
  }
}

function requestHeaders(init?: RequestInit) {
  const headers = new Headers(init?.headers);
  headers.set("Content-Type", "application/json");
  if (authToken) headers.set("Authorization", `Bearer ${authToken}`);
  return headers;
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(`/v1${path}`, {
    ...init,
    headers: requestHeaders(init),
  });
  if (!resp.ok) {
    const body = await resp.text();
    throw new ApiError(resp.status, body);
  }
  return (await resp.json()) as T;
}

export const api = {
  setToken: (token: string | null) => {
    authToken = token ?? "";
  },
  health: () => req<Health>("/health"),
  signup: (body: AuthRequest) =>
    req<{ id: string; email: string }>("/auth/signup", { method: "POST", body: JSON.stringify(body) }),
  login: (body: AuthRequest) =>
    req<AuthLoginResponse>("/auth/login", { method: "POST", body: JSON.stringify(body) }),
  logout: () => req<{ status: string }>("/auth/logout", { method: "POST", body: "{}" }),
  dbStatus: () => req<DbStatus>("/db/status"),
  listTasks: () => req<Task[]>("/tasks"),
  createTask: (body: CreateTaskRequest) =>
    req<Task>("/tasks", { method: "POST", body: JSON.stringify(body) }),
  taskState: (id: string) => req<WorkingState>(`/tasks/${id}/state`),
  taskSessions: (id: string) => req<string[]>(`/tasks/${id}/sessions`),
  transitionTask: (id: string, action: TaskAction) =>
    req<Task>(`/tasks/${id}/${action}`, { method: "POST", body: "{}" }),
  deleteTask: (id: string) => req<{ forgot: string }>(`/tasks/${id}`, { method: "DELETE" }),
  sessionEvents: (id: string) => req<SessionEvent[]>(`/sessions/${id}/events`),
  deleteSession: (id: string) =>
    req<{ forgot: string }>(`/sessions/${id}`, { method: "DELETE" }),
  ingest: () => req<IngestResult[]>("/ingest", { method: "POST" }),
  resume: (body: ResumeRequest) =>
    req<RenderedResume>("/resume", { method: "POST", body: JSON.stringify(body) }),
};
