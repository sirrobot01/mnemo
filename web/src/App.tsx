import { useCallback, useEffect, useState } from "react";
import { api } from "./api/client";
import type { SessionEvent, Task, WorkingState } from "./api/types";

export default function App() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [state, setState] = useState<WorkingState | null>(null);
  const [events, setEvents] = useState<SessionEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const loadTasks = useCallback(async () => {
    try {
      setTasks(await api.listTasks());
    } catch (e) {
      setError(String(e));
    }
  }, []);

  useEffect(() => {
    void loadTasks();
  }, [loadTasks]);

  const openTask = useCallback(async (id: string) => {
    setSelected(id);
    setState(null);
    setEvents([]);
    setError(null);
    try {
      setState(await api.taskState(id));
    } catch {
      /* no compiled state yet — not an error */
    }
    try {
      const sids = await api.taskSessions(id);
      const all: SessionEvent[] = [];
      for (const sid of sids) all.push(...(await api.sessionEvents(sid)));
      all.sort((a, b) => a.timestamp.localeCompare(b.timestamp));
      setEvents(all);
    } catch (e) {
      setError(String(e));
    }
  }, []);

  const ingest = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      await api.ingest();
      await loadTasks();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }, [loadTasks]);

  return (
    <div className="app">
      <aside className="sidebar">
        <header>
          <h1>mnemo</h1>
          <button onClick={() => void ingest()} disabled={busy}>
            {busy ? "Ingesting…" : "Ingest"}
          </button>
        </header>
        <ul className="tasks">
          {tasks.map((t) => (
            <li
              key={t.id}
              className={t.id === selected ? "task active" : "task"}
              onClick={() => void openTask(t.id)}
            >
              <span className={`pill ${t.status}`}>{t.status}</span>
              <span className="title">{t.title}</span>
            </li>
          ))}
          {tasks.length === 0 && <li className="empty">No tasks. Ingest to begin.</li>}
        </ul>
      </aside>

      <main className="detail">
        {error && <div className="error">{error}</div>}
        {!selected && <p className="empty">Select a task.</p>}
        {selected && (
          <>
            <section className="sop">
              <h2>State of play{state ? ` — v${state.version}` : ""}</h2>
              {!state && <p className="empty">No compiled state. Run resume/ingest.</p>}
              {state && (
                <div className="grid">
                  <Block title="Goal" items={state.goal ? [state.goal] : []} />
                  <Block title="Done" items={state.done ?? []} />
                  <Block title="Next steps" items={state.next_steps ?? []} />
                  <Block
                    title="Rejected — do not retry"
                    items={(state.rejected ?? []).map((r) => `${r.approach} — ${r.reason ?? ""}`)}
                  />
                  <Block
                    title="Decisions"
                    items={(state.decisions ?? []).map((d) => d.decision)}
                  />
                  <Block title="Open questions" items={state.open_questions ?? []} />
                  <Block
                    title="Files touched"
                    items={(state.files_touched ?? []).map((f) => f.path)}
                  />
                  <Block
                    title="Hypotheses (UNCONFIRMED)"
                    items={(state.hypotheses ?? []).map((h) => h.claim)}
                  />
                </div>
              )}
            </section>

            <section className="timeline">
              <h2>Session timeline ({events.length})</h2>
              {events.map((e) => (
                <div key={e.id} className={`event ${e.type}`}>
                  <span className="etype">{e.type}</span>
                  <span className="econtent">{e.content.slice(0, 400)}</span>
                </div>
              ))}
            </section>
          </>
        )}
      </main>
    </div>
  );
}

function Block({ title, items }: { title: string; items: string[] }) {
  if (items.length === 0) return null;
  return (
    <div className="block">
      <h3>{title}</h3>
      <ul>
        {items.map((it, i) => (
          <li key={i}>{it}</li>
        ))}
      </ul>
    </div>
  );
}
