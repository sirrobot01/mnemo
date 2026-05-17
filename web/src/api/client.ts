import type { IngestResult, SessionEvent, Task, WorkingState } from "./types";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(`/v1${path}`, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!resp.ok) {
    const body = await resp.text();
    throw new Error(`${resp.status}: ${body}`);
  }
  return (await resp.json()) as T;
}

export const api = {
  listTasks: () => req<Task[]>("/tasks"),
  taskState: (id: string) => req<WorkingState>(`/tasks/${id}/state`),
  taskSessions: (id: string) => req<string[]>(`/tasks/${id}/sessions`),
  sessionEvents: (id: string) => req<SessionEvent[]>(`/sessions/${id}/events`),
  ingest: () => req<IngestResult[]>("/ingest", { method: "POST" }),
};
