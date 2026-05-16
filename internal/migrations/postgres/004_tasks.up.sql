CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL REFERENCES repositories(id),
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL,
  ended_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE task_sessions (
  task_id TEXT NOT NULL REFERENCES tasks(id),
  session_id TEXT NOT NULL REFERENCES sessions(id),
  attached_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (task_id, session_id)
);

CREATE TABLE task_memories (
  task_id TEXT NOT NULL REFERENCES tasks(id),
  memory_id TEXT NOT NULL REFERENCES memories(id),
  attached_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (task_id, memory_id)
);

CREATE INDEX IF NOT EXISTS idx_tasks_repo_status ON tasks(repo_id, status);
CREATE INDEX IF NOT EXISTS idx_task_sessions_session ON task_sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_task_memories_memory ON task_memories(memory_id);
