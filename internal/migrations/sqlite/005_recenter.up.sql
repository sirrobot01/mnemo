-- Re-center: cross-agent task continuity. Drops the entire memory/rules
-- product and rebuilds tasks in the new shape. Destructive by design — the
-- prior product's data has no meaning in the new model.

DROP TABLE IF EXISTS task_memories;
DROP TABLE IF EXISTS task_sessions;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS memory_tags;
DROP TABLE IF EXISTS memory_evidence;
DROP TABLE IF EXISTS proposals;
DROP TABLE IF EXISTS conflicts;
DROP TABLE IF EXISTS projections;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS memories;
DROP TABLE IF EXISTS users;

CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL REFERENCES repositories(id),
  title TEXT NOT NULL,
  goal TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_active_at TEXT NOT NULL
);

CREATE TABLE task_sessions (
  task_id TEXT NOT NULL REFERENCES tasks(id),
  session_id TEXT NOT NULL REFERENCES sessions(id),
  attached_at TEXT NOT NULL,
  PRIMARY KEY (task_id, session_id)
);

CREATE TABLE working_states (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id),
  version INTEGER NOT NULL,
  compiled_at TEXT NOT NULL,
  source_watermark TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_repo_status ON tasks(repo_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_last_active ON tasks(last_active_at);
CREATE INDEX IF NOT EXISTS idx_task_sessions_session ON task_sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_working_states_task ON working_states(task_id, version);
