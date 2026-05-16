CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  ended_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (repo_id) REFERENCES repositories(id)
);

CREATE TABLE task_sessions (
  task_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  attached_at TEXT NOT NULL,
  PRIMARY KEY (task_id, session_id),
  FOREIGN KEY (task_id) REFERENCES tasks(id),
  FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE task_memories (
  task_id TEXT NOT NULL,
  memory_id TEXT NOT NULL,
  attached_at TEXT NOT NULL,
  PRIMARY KEY (task_id, memory_id),
  FOREIGN KEY (task_id) REFERENCES tasks(id),
  FOREIGN KEY (memory_id) REFERENCES memories(id)
);

CREATE INDEX IF NOT EXISTS idx_tasks_repo_status ON tasks(repo_id, status);
CREATE INDEX IF NOT EXISTS idx_task_sessions_session ON task_sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_task_memories_memory ON task_memories(memory_id);
