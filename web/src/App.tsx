import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { ApiError, api } from "./api/client";
import type {
  DbStatus,
  Health,
  IngestResult,
  RenderedResume,
  SessionEvent,
  Task,
  TaskAction,
  WorkingState,
} from "./api/types";

type StatusFilter = "all" | Task["status"];
type Panel = "state" | "timeline" | "resume";
type AuthMode = "login" | "signup";

const authStorageKey = "mnemo.auth.token";

const statusFilters: StatusFilter[] = ["all", "active", "paused", "done"];
const resumeTargets = [
  { value: "generic", label: "Generic handoff" },
  { value: "claude", label: "Claude" },
  { value: "codex", label: "Codex" },
  { value: "aider", label: "Aider" },
  { value: "continue", label: "Continue" },
];

export default function App() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [state, setState] = useState<WorkingState | null>(null);
  const [sessionIds, setSessionIds] = useState<string[]>([]);
  const [events, setEvents] = useState<SessionEvent[]>([]);
  const [health, setHealth] = useState<Health | null>(null);
  const [db, setDb] = useState<DbStatus | null>(null);
  const [lastIngest, setLastIngest] = useState<IngestResult[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [taskQuery, setTaskQuery] = useState("");
  const [eventType, setEventType] = useState("all");
  const [eventQuery, setEventQuery] = useState("");
  const [panel, setPanel] = useState<Panel>("state");
  const [showNewTask, setShowNewTask] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [newGoal, setNewGoal] = useState("");
  const [newBranch, setNewBranch] = useState("");
  const [resumeTool, setResumeTool] = useState("generic");
  const [allowCrossVendor, setAllowCrossVendor] = useState(false);
  const [resume, setResume] = useState<RenderedResume | null>(null);
  const [copied, setCopied] = useState<string | null>(null);
  const [authToken, setAuthTokenState] = useState(() => window.localStorage.getItem(authStorageKey));
  const [authMode, setAuthMode] = useState<AuthMode>("login");
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [authBusy, setAuthBusy] = useState(false);

  const saveAuthToken = useCallback((token: string | null) => {
    if (token) {
      window.localStorage.setItem(authStorageKey, token);
    } else {
      window.localStorage.removeItem(authStorageKey);
    }
    api.setToken(token);
    setAuthTokenState(token);
  }, []);

  useEffect(() => {
    api.setToken(authToken);
  }, [authToken]);

  const handleError = useCallback(
    (e: unknown) => {
      if (e instanceof ApiError && e.status === 401) {
        saveAuthToken(null);
        setError("Sign in to continue.");
        return;
      }
      setError(errorMessage(e));
    },
    [saveAuthToken],
  );

  const selectedTask = useMemo(
    () => tasks.find((task) => task.id === selected) ?? null,
    [selected, tasks],
  );

  const statusCounts = useMemo(() => {
    return {
      all: tasks.length,
      active: tasks.filter((task) => task.status === "active").length,
      paused: tasks.filter((task) => task.status === "paused").length,
      done: tasks.filter((task) => task.status === "done").length,
    };
  }, [tasks]);

  const filteredTasks = useMemo(() => {
    const query = taskQuery.trim().toLowerCase();
    return tasks
      .filter((task) => statusFilter === "all" || task.status === statusFilter)
      .filter((task) => {
        if (!query) return true;
        return [task.title, task.goal, task.branch, task.id]
          .filter(Boolean)
          .some((value) => value!.toLowerCase().includes(query));
      })
      .sort((a, b) => {
        if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
        const statusRank = { active: 0, paused: 1, done: 2 };
        if (statusRank[a.status] !== statusRank[b.status]) {
          return statusRank[a.status] - statusRank[b.status];
        }
        return Date.parse(b.last_active_at) - Date.parse(a.last_active_at);
      });
  }, [statusFilter, taskQuery, tasks]);

  const eventTypes = useMemo(() => {
    const types = Array.from(new Set(events.map((event) => event.type))).sort();
    return ["all", ...types];
  }, [events]);

  const filteredEvents = useMemo(() => {
    const query = eventQuery.trim().toLowerCase();
    return events.filter((event) => {
      const typeMatch = eventType === "all" || event.type === eventType;
      const textMatch =
        !query ||
        event.content.toLowerCase().includes(query) ||
        event.session_id.toLowerCase().includes(query);
      return typeMatch && textMatch;
    });
  }, [eventQuery, eventType, events]);

  const loadOverview = useCallback(async () => {
    const nextHealth = await api.health();
    setHealth(nextHealth);
    if (nextHealth.auth_required && !authToken) {
      setDb(null);
      return nextHealth;
    }
    setDb(await api.dbStatus());
    return nextHealth;
  }, [authToken]);

  const loadTasks = useCallback(async () => {
    const next = await api.listTasks();
    setTasks(next);
    return next;
  }, []);

  const openTask = useCallback(async (id: string) => {
    setSelected(id);
    setState(null);
    setEvents([]);
    setSessionIds([]);
    setResume(null);
    setError(null);
    setDetailLoading(true);

    try {
      const [stateResult, sessionsResult] = await Promise.allSettled([
        api.taskState(id),
        api.taskSessions(id),
      ]);

      if (stateResult.status === "fulfilled") {
        setState(stateResult.value);
      } else if (stateResult.reason instanceof ApiError && stateResult.reason.status === 401) {
        throw stateResult.reason;
      }

      if (sessionsResult.status === "rejected") {
        throw sessionsResult.reason;
      }

      const ids = sessionsResult.value;
      setSessionIds(ids);

      const eventResults = await Promise.allSettled(ids.map((sid) => api.sessionEvents(sid)));
      const nextEvents = eventResults.flatMap((result) =>
        result.status === "fulfilled" ? result.value : [],
      );
      nextEvents.sort((a, b) => a.timestamp.localeCompare(b.timestamp));
      setEvents(nextEvents);

      const failed = eventResults.some((result) => result.status === "rejected");
      const unauthorized = eventResults.find(
        (result) =>
          result.status === "rejected" &&
          result.reason instanceof ApiError &&
          result.reason.status === 401,
      );
      if (unauthorized?.status === "rejected") {
        throw unauthorized.reason;
      }
      if (failed) {
        setError("Some session events could not be loaded.");
      }
    } catch (e) {
      handleError(e);
    } finally {
      setDetailLoading(false);
    }
  }, [handleError]);

  useEffect(() => {
    let cancelled = false;
    const boot = async () => {
      setError(null);
      try {
        const nextHealth = await loadOverview();
        if (cancelled || (nextHealth.auth_required && !authToken)) return;
        await loadTasks();
      } catch (e) {
        if (!cancelled) handleError(e);
      }
    };
    void boot();
    return () => {
      cancelled = true;
    };
  }, [authToken, handleError, loadOverview, loadTasks]);

  useEffect(() => {
    if (tasks.length === 0) return;
    if (selected && tasks.some((task) => task.id === selected)) return;
    const preferred =
      tasks.find((task) => task.pinned && task.status !== "done") ??
      tasks.find((task) => task.status === "active") ??
      tasks[0];
    void openTask(preferred.id);
  }, [openTask, selected, tasks]);

  const refresh = useCallback(async () => {
    setBusyAction("refresh");
    setError(null);
    try {
      const nextHealth = await loadOverview();
      if (nextHealth.auth_required && !authToken) return;
      const next = await loadTasks();
      const id = selected && next.some((task) => task.id === selected) ? selected : next[0]?.id;
      if (id) await openTask(id);
      setNotice("View refreshed.");
    } catch (e) {
      handleError(e);
    } finally {
      setBusyAction(null);
    }
  }, [authToken, handleError, loadOverview, loadTasks, openTask, selected]);

  const ingest = useCallback(async () => {
    setBusyAction("ingest");
    setError(null);
    setNotice(null);
    try {
      const results = await api.ingest();
      setLastIngest(results);
      const next = await loadTasks();
      const id = selected && next.some((task) => task.id === selected) ? selected : next[0]?.id;
      if (id) await openTask(id);
      setNotice(ingestSummary(results));
    } catch (e) {
      handleError(e);
    } finally {
      setBusyAction(null);
    }
  }, [handleError, loadTasks, openTask, selected]);

  const createTask = async (event: FormEvent) => {
    event.preventDefault();
    if (!newTitle.trim()) return;
    setBusyAction("create");
    setError(null);
    try {
      const task = await api.createTask({
        title: newTitle,
        goal: newGoal,
        branch: newBranch,
      });
      setNewTitle("");
      setNewGoal("");
      setNewBranch("");
      setShowNewTask(false);
      await loadTasks();
      await openTask(task.id);
      setNotice("Task created and made active.");
    } catch (e) {
      handleError(e);
    } finally {
      setBusyAction(null);
    }
  };

  const transitionTask = async (action: TaskAction) => {
    if (!selectedTask) return;
    setBusyAction(action);
    setError(null);
    try {
      const task = await api.transitionTask(selectedTask.id, action);
      setTasks((current) => current.map((item) => (item.id === task.id ? task : item)));
      await loadTasks();
      await openTask(task.id);
      setNotice(actionNotice(action));
    } catch (e) {
      handleError(e);
    } finally {
      setBusyAction(null);
    }
  };

  const forgetSelectedTask = async () => {
    if (!selectedTask) return;
    const confirmed = window.confirm(
      `Forget "${selectedTask.title}"? This removes the task, its compiled states, and session links.`,
    );
    if (!confirmed) return;
    setBusyAction("forget-task");
    setError(null);
    try {
      await api.deleteTask(selectedTask.id);
      setSelected(null);
      setState(null);
      setEvents([]);
      setSessionIds([]);
      await loadTasks();
      setNotice("Task forgotten.");
    } catch (e) {
      handleError(e);
    } finally {
      setBusyAction(null);
    }
  };

  const renderResume = async () => {
    if (!selectedTask) return;
    setBusyAction("resume");
    setError(null);
    setNotice(null);
    try {
      const rendered = await api.resume({
        task_id: selectedTask.id,
        tool: resumeTool === "generic" ? "" : resumeTool,
        allow_cross_vendor: allowCrossVendor,
      });
      setResume(rendered);
      setPanel("resume");
      try {
        setState(await api.taskState(selectedTask.id));
      } catch {
        /* Rendering can fail to persist state only if the API returned an error above. */
      }
    } catch (e) {
      handleError(e);
    } finally {
      setBusyAction(null);
    }
  };

  const forgetSession = async (sessionID: string) => {
    const confirmed = window.confirm(
      `Forget session ${shortId(sessionID)}? This removes the session and its events from Mnemo.`,
    );
    if (!confirmed) return;
    setBusyAction(`forget-${sessionID}`);
    setError(null);
    try {
      await api.deleteSession(sessionID);
      setSessionIds((current) => current.filter((id) => id !== sessionID));
      setEvents((current) => current.filter((event) => event.session_id !== sessionID));
      await loadTasks();
      setNotice("Session forgotten.");
    } catch (e) {
      handleError(e);
    } finally {
      setBusyAction(null);
    }
  };

  const submitAuth = async (event: FormEvent) => {
    event.preventDefault();
    if (!authEmail.trim() || !authPassword) return;
    setAuthBusy(true);
    setError(null);
    try {
      const credentials = { email: authEmail.trim(), password: authPassword };
      if (authMode === "signup" && health?.allow_signup) {
        await api.signup(credentials);
      }
      const signedIn = await api.login(credentials);
      saveAuthToken(signedIn.token);
      setAuthPassword("");
      setNotice("Signed in.");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setAuthBusy(false);
    }
  };

  const signOut = async () => {
    try {
      await api.logout();
    } catch {
      /* Token may already be expired; local cleanup is still the right result. */
    }
    saveAuthToken(null);
    setTasks([]);
    setSelected(null);
    setState(null);
    setEvents([]);
    setSessionIds([]);
    setResume(null);
    setNotice(null);
  };

  const copyText = async (key: string, value: string) => {
    await copyToClipboard(value);
    setCopied(key);
    window.setTimeout(() => setCopied((current) => (current === key ? null : current)), 1200);
  };

  if (health?.auth_required && !authToken) {
    return (
      <AuthScreen
        allowSignup={!!health.allow_signup}
        busy={authBusy}
        email={authEmail}
        error={error}
        mode={authMode}
        onEmail={setAuthEmail}
        onMode={setAuthMode}
        onPassword={setAuthPassword}
        onSubmit={(event) => void submitAuth(event)}
        password={authPassword}
      />
    );
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand">
          <div>
            <strong>mnemo</strong>
            <span>{health?.repository ? `Repository ${shortId(health.repository)}` : "Local continuity"}</span>
          </div>
        </div>
        <div className="topbar-status" aria-label="System status">
          <span>{db?.backend ?? "unknown"} database</span>
          {typeof db?.pending === "number" && <span>{db.pending} pending migrations</span>}
          {lastIngest.length > 0 && <span>{lastIngest.length} adapters checked</span>}
        </div>
        <div className="topbar-actions">
          <button className="button secondary" type="button" onClick={() => void refresh()} disabled={!!busyAction}>
            {busyAction === "refresh" ? "Refreshing..." : "Refresh"}
          </button>
          <button className="button primary" type="button" onClick={() => void ingest()} disabled={!!busyAction}>
            {busyAction === "ingest" ? "Ingesting..." : "Ingest"}
          </button>
          {health?.auth_required && (
            <button className="button secondary" type="button" onClick={() => void signOut()}>
              Sign out
            </button>
          )}
        </div>
      </header>

      <div className="workspace">
        <aside className="task-sidebar">
          <div className="sidebar-head">
            <div>
              <h2>Tasks</h2>
              <p>{tasks.length === 0 ? "No tasks yet" : `${tasks.length} total tasks`}</p>
            </div>
            <button
              className="button compact"
              type="button"
              onClick={() => setShowNewTask((showing) => !showing)}
            >
              New task
            </button>
          </div>

          {showNewTask && (
            <form className="new-task" onSubmit={(event) => void createTask(event)}>
              <label>
                <span>Title</span>
                <input
                  autoFocus
                  value={newTitle}
                  onChange={(event) => setNewTitle(event.target.value)}
                  placeholder="Ship the new dashboard"
                />
              </label>
              <label>
                <span>Goal</span>
                <textarea
                  value={newGoal}
                  onChange={(event) => setNewGoal(event.target.value)}
                  placeholder="What should the next agent preserve?"
                  rows={3}
                />
              </label>
              <label>
                <span>Branch</span>
                <input
                  value={newBranch}
                  onChange={(event) => setNewBranch(event.target.value)}
                  placeholder="optional"
                />
              </label>
              <div className="form-actions">
                <button className="button secondary" type="button" onClick={() => setShowNewTask(false)}>
                  Cancel
                </button>
                <button className="button primary" type="submit" disabled={busyAction === "create" || !newTitle.trim()}>
                  {busyAction === "create" ? "Creating..." : "Create"}
                </button>
              </div>
            </form>
          )}

          <label className="search-field">
            <span>Search tasks</span>
            <input
              value={taskQuery}
              onChange={(event) => setTaskQuery(event.target.value)}
              placeholder="Title, branch, goal, or id"
            />
          </label>

          <div className="status-tabs" role="tablist" aria-label="Task status filters">
            {statusFilters.map((filter) => (
              <button
                key={filter}
                className={statusFilter === filter ? "active" : ""}
                type="button"
                onClick={() => setStatusFilter(filter)}
              >
                <span>{filter}</span>
                <b>{statusCounts[filter]}</b>
              </button>
            ))}
          </div>

          <div className="task-list" role="list">
            {filteredTasks.map((task) => (
              <button
                key={task.id}
                className={task.id === selected ? "task-row selected" : "task-row"}
                type="button"
                onClick={() => void openTask(task.id)}
              >
                <span className="task-row-body">
                  <span className="task-row-top">
                    <span className="task-title">{task.title}</span>
                    <StatusPill status={task.status} />
                  </span>
                  <span className="task-row-meta">
                    {task.pinned && <span className="pin">Pinned</span>}
                    {task.branch && <span>{task.branch}</span>}
                    <span>{relativeTime(task.last_active_at)}</span>
                  </span>
                  {task.goal && <span className="task-goal">{task.goal}</span>}
                </span>
              </button>
            ))}
            {filteredTasks.length === 0 && (
              <div className="empty slim">
                <strong>No matching tasks</strong>
                <span>Adjust the search or status filter.</span>
              </div>
            )}
          </div>
        </aside>

        <main className="main-panel">
          {error && (
            <div className="alert error">
              <strong>Action failed</strong>
              <span>{error}</span>
            </div>
          )}
          {notice && !error && (
            <div className="alert success">
              <strong>Done</strong>
              <span>{notice}</span>
            </div>
          )}

          {!selectedTask && (
            <EmptyState
              title="Select or create a task"
              body="Ingest sessions to discover work, or start a manual task to pin the next agent to a clear state of play."
            />
          )}

          {selectedTask && (
            <>
              <section className="task-hero">
                <div className="hero-copy">
                  <div className="hero-kicker">
                    <StatusPill status={selectedTask.status} />
                    {selectedTask.pinned && <span className="badge accent">Pinned active task</span>}
                    {selectedTask.branch && <span className="badge">{selectedTask.branch}</span>}
                  </div>
                  <h1>{selectedTask.title}</h1>
                  <p>{selectedTask.goal || state?.goal || "No goal captured yet."}</p>
                  <div className="hero-meta">
                    <span>ID {shortId(selectedTask.id)}</span>
                    <span>Last active {relativeTime(selectedTask.last_active_at)}</span>
                    <span>Updated {formatDate(selectedTask.updated_at)}</span>
                  </div>
                </div>
                <div className="hero-actions">
                  <button
                    className="button primary"
                    type="button"
                    onClick={() => void renderResume()}
                    disabled={busyAction === "resume"}
                  >
                    {busyAction === "resume" ? "Rendering..." : "Render resume"}
                  </button>
                  <button
                    className="button secondary"
                    type="button"
                    onClick={() => void transitionTask("switch")}
                    disabled={selectedTask.status !== "paused" || busyAction === "switch"}
                  >
                    Make active
                  </button>
                  <button
                    className="button secondary"
                    type="button"
                    onClick={() => void transitionTask("pause")}
                    disabled={selectedTask.status !== "active" || busyAction === "pause"}
                  >
                    Pause
                  </button>
                  <button
                    className="button secondary"
                    type="button"
                    onClick={() => void transitionTask("done")}
                    disabled={selectedTask.status === "done" || busyAction === "done"}
                  >
                    Mark done
                  </button>
                  <button
                    className="button danger"
                    type="button"
                    onClick={() => void forgetSelectedTask()}
                    disabled={busyAction === "forget-task"}
                  >
                    Forget
                  </button>
                </div>
              </section>

              <section className="metrics" aria-label="Task metrics">
                <Metric label="Sessions" value={sessionIds.length} />
                <Metric label="Events" value={events.length} />
                <Metric label="State version" value={state?.version ?? "-"} />
                <Metric label="Next steps" value={state?.next_steps?.length ?? 0} />
              </section>

              <div className="panel-tabs" role="tablist" aria-label="Task detail panels">
                <button className={panel === "state" ? "active" : ""} type="button" onClick={() => setPanel("state")}>
                  State of play
                </button>
                <button
                  className={panel === "timeline" ? "active" : ""}
                  type="button"
                  onClick={() => setPanel("timeline")}
                >
                  Timeline
                </button>
                <button
                  className={panel === "resume" ? "active" : ""}
                  type="button"
                  onClick={() => setPanel("resume")}
                >
                  Resume
                </button>
              </div>

              {detailLoading && <div className="loading-line" />}

              {panel === "state" && <StatePanel state={state} />}
              {panel === "timeline" && (
                <TimelinePanel
                  copied={copied}
                  eventQuery={eventQuery}
                  eventType={eventType}
                  eventTypes={eventTypes}
                  events={filteredEvents}
                  onCopy={(key, value) => void copyText(key, value)}
                  onForgetSession={(sessionID) => void forgetSession(sessionID)}
                  setEventQuery={setEventQuery}
                  setEventType={setEventType}
                  totalEvents={events.length}
                />
              )}
              {panel === "resume" && (
                <ResumePanel
                  allowCrossVendor={allowCrossVendor}
                  busy={busyAction === "resume"}
                  copied={copied === "resume"}
                  onCopy={() => resume?.content && void copyText("resume", resume.content)}
                  onRender={() => void renderResume()}
                  resume={resume}
                  resumeTool={resumeTool}
                  setAllowCrossVendor={setAllowCrossVendor}
                  setResumeTool={setResumeTool}
                />
              )}
            </>
          )}
        </main>
      </div>
    </div>
  );
}

function AuthScreen({
  allowSignup,
  busy,
  email,
  error,
  mode,
  onEmail,
  onMode,
  onPassword,
  onSubmit,
  password,
}: {
  allowSignup: boolean;
  busy: boolean;
  email: string;
  error: string | null;
  mode: AuthMode;
  onEmail: (value: string) => void;
  onMode: (value: AuthMode) => void;
  onPassword: (value: string) => void;
  onSubmit: (event: FormEvent) => void;
  password: string;
}) {
  const canSignup = allowSignup;
  const effectiveMode = mode === "signup" && !canSignup ? "login" : mode;
  return (
    <main className="auth-page">
      <form className="auth-panel" onSubmit={onSubmit}>
        <div className="auth-brand">
          <strong>mnemo</strong>
          <span>Repository continuity</span>
        </div>
        {error && (
          <div className="alert error">
            <strong>Authentication failed</strong>
            <span>{error}</span>
          </div>
        )}
        {canSignup && (
          <div className="auth-tabs" role="tablist" aria-label="Authentication mode">
            <button
              className={effectiveMode === "login" ? "active" : ""}
              type="button"
              onClick={() => onMode("login")}
            >
              Sign in
            </button>
            <button
              className={effectiveMode === "signup" ? "active" : ""}
              type="button"
              onClick={() => onMode("signup")}
            >
              Create account
            </button>
          </div>
        )}
        <label className="auth-field">
          <span>Email</span>
          <input
            autoFocus
            autoComplete="email"
            type="email"
            value={email}
            onChange={(event) => onEmail(event.target.value)}
          />
        </label>
        <label className="auth-field">
          <span>Password</span>
          <input
            autoComplete={effectiveMode === "signup" ? "new-password" : "current-password"}
            type="password"
            value={password}
            onChange={(event) => onPassword(event.target.value)}
          />
        </label>
        <button className="button primary auth-submit" type="submit" disabled={busy || !email.trim() || !password}>
          {busy ? "Working..." : effectiveMode === "signup" ? "Create account" : "Sign in"}
        </button>
      </form>
    </main>
  );
}

function StatePanel({ state }: { state: WorkingState | null }) {
  if (!state) {
    return (
      <EmptyState
        title="No compiled state yet"
        body="Render a resume or run ingest/resume to compile the latest state of play for this task."
      />
    );
  }

  const blocks = [
    { title: "Goal", items: state.goal ? [state.goal] : [] },
    { title: "In progress", items: state.in_progress ? [state.in_progress] : [] },
    { title: "Next steps", items: state.next_steps ?? [] },
    { title: "Done", items: state.done ?? [] },
    {
      title: "Decisions",
      items: (state.decisions ?? []).map((item) =>
        item.rationale ? `${item.decision} - ${item.rationale}` : item.decision,
      ),
    },
    { title: "Open questions", items: state.open_questions ?? [] },
    {
      title: "Files touched",
      items: (state.files_touched ?? []).map((item) =>
        item.summary ? `${item.path} - ${item.summary}` : item.path,
      ),
    },
    {
      title: "Rejected approaches",
      items: (state.rejected ?? []).map((item) =>
        item.reason ? `${item.approach} - ${item.reason}` : item.approach,
      ),
    },
    {
      title: "Hypotheses",
      items: (state.hypotheses ?? []).map((item) => {
        const confidence = item.confidence ? `, ${Math.round(item.confidence * 100)}% confidence` : "";
        return item.confirmed ? item.claim : `${item.claim} (unconfirmed${confidence})`;
      }),
    },
  ].filter((block) => block.items.length > 0);

  if (blocks.length === 0) {
    return <EmptyState title="State is empty" body="The compiler did not find durable continuity notes yet." />;
  }

  return (
    <section className="state-grid">
      {blocks.map((block) => (
        <article className="state-block" key={block.title}>
          <h3>{block.title}</h3>
          <ul>
            {block.items.map((item, index) => (
              <li key={index}>{item}</li>
            ))}
          </ul>
        </article>
      ))}
    </section>
  );
}

function TimelinePanel({
  copied,
  eventQuery,
  eventType,
  eventTypes,
  events,
  onCopy,
  onForgetSession,
  setEventQuery,
  setEventType,
  totalEvents,
}: {
  copied: string | null;
  eventQuery: string;
  eventType: string;
  eventTypes: string[];
  events: SessionEvent[];
  onCopy: (key: string, value: string) => void;
  onForgetSession: (sessionID: string) => void;
  setEventQuery: (value: string) => void;
  setEventType: (value: string) => void;
  totalEvents: number;
}) {
  return (
    <section className="timeline-panel">
      <div className="timeline-tools">
        <label className="search-field">
          <span>Search events</span>
          <input
            value={eventQuery}
            onChange={(event) => setEventQuery(event.target.value)}
            placeholder="Message text or session id"
          />
        </label>
        <label className="select-field">
          <span>Type</span>
          <select value={eventType} onChange={(event) => setEventType(event.target.value)}>
            {eventTypes.map((type) => (
              <option key={type} value={type}>
                {type === "all" ? "All events" : typeLabel(type)}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div className="section-title">
        <h3>Session timeline</h3>
        <span>
          {events.length} of {totalEvents} events
        </span>
      </div>

      <div className="event-list">
        {events.map((event) => {
          const key = `event-${event.id}`;
          const long = event.content.length > 700;
          return (
            <article className="event-card" key={event.id}>
              <div className="event-head">
                <span className={`event-type ${eventTone(event.type)}`}>{typeLabel(event.type)}</span>
                <span>{formatDate(event.timestamp)}</span>
                <span>Session {shortId(event.session_id)}</span>
              </div>
              <pre>{long ? `${event.content.slice(0, 700)}...` : event.content}</pre>
              {long && (
                <details>
                  <summary>Show full event</summary>
                  <pre>{event.content}</pre>
                </details>
              )}
              <div className="event-actions">
                <button className="text-button" type="button" onClick={() => onCopy(key, event.content)}>
                  {copied === key ? "Copied" : "Copy event"}
                </button>
                <button className="text-button danger-text" type="button" onClick={() => onForgetSession(event.session_id)}>
                  Forget session
                </button>
              </div>
            </article>
          );
        })}
        {events.length === 0 && (
          <EmptyState title="No matching events" body="Change the event filter or ingest more sessions." />
        )}
      </div>
    </section>
  );
}

function ResumePanel({
  allowCrossVendor,
  busy,
  copied,
  onCopy,
  onRender,
  resume,
  resumeTool,
  setAllowCrossVendor,
  setResumeTool,
}: {
  allowCrossVendor: boolean;
  busy: boolean;
  copied: boolean;
  onCopy: () => void;
  onRender: () => void;
  resume: RenderedResume | null;
  resumeTool: string;
  setAllowCrossVendor: (value: boolean) => void;
  setResumeTool: (value: string) => void;
}) {
  return (
    <section className="resume-panel">
      <div className="resume-toolbar">
        <label className="select-field">
          <span>Target</span>
          <select value={resumeTool} onChange={(event) => setResumeTool(event.target.value)}>
            {resumeTargets.map((target) => (
              <option key={target.value} value={target.value}>
                {target.label}
              </option>
            ))}
          </select>
        </label>
        <label className="check-field">
          <input
            type="checkbox"
            checked={allowCrossVendor}
            onChange={(event) => setAllowCrossVendor(event.target.checked)}
          />
          <span>Allow cross-vendor handoff</span>
        </label>
        <button className="button primary" type="button" onClick={onRender} disabled={busy}>
          {busy ? "Rendering..." : "Render"}
        </button>
        <button className="button secondary" type="button" onClick={onCopy} disabled={!resume?.content}>
          {copied ? "Copied" : "Copy"}
        </button>
      </div>

      {resume?.content ? (
        <textarea className="resume-output" readOnly value={resume.content} />
      ) : (
        <EmptyState
          title="No resume rendered"
          body="Choose a target and render a handoff block for the next agent."
        />
      )}
    </section>
  );
}

function Metric({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function StatusPill({ status }: { status: Task["status"] }) {
  return <span className={`status-pill ${status}`}>{status}</span>;
}

function EmptyState({ title, body }: { title: string; body: string }) {
  return (
    <div className="empty">
      <strong>{title}</strong>
      <span>{body}</span>
    </div>
  );
}

function actionNotice(action: TaskAction) {
  switch (action) {
    case "switch":
      return "Task is now active and pinned.";
    case "pause":
      return "Task paused.";
    case "done":
      return "Task marked done.";
  }
}

function ingestSummary(results: IngestResult[]) {
  const totals = results.reduce(
    (acc, item) => {
      acc.discovered += item.discovered;
      acc.imported += item.imported;
      acc.unchanged += item.unchanged ?? 0;
      acc.skipped += item.skipped ?? 0;
      return acc;
    },
    { discovered: 0, imported: 0, unchanged: 0, skipped: 0 },
  );
  return `Ingest complete: ${totals.imported} imported, ${totals.unchanged} unchanged, ${totals.skipped} skipped from ${totals.discovered} discovered.`;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function typeLabel(type: string) {
  return type.replaceAll("_", " ");
}

function eventTone(type: string) {
  if (type.includes("user")) return "user";
  if (type.includes("assistant")) return "assistant";
  if (type.includes("tool")) return "tool";
  if (type.includes("thinking")) return "thinking";
  return "system";
}

function shortId(id: string) {
  if (!id) return "-";
  return id.length > 12 ? id.slice(0, 12) : id;
}

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function relativeTime(value: string) {
  const time = Date.parse(value);
  if (Number.isNaN(time)) return "unknown";
  const seconds = Math.max(0, Math.round((Date.now() - time) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}

async function copyToClipboard(value: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand("copy");
  document.body.removeChild(textarea);
}
