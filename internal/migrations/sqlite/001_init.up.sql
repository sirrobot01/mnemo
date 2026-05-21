CREATE TABLE repositories (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL,
  remote_url TEXT NOT NULL DEFAULT '',
  default_branch TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL,
  agent TEXT NOT NULL,
  kind TEXT NOT NULL,
  source_path TEXT NOT NULL,
  external_id TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  ended_at TEXT,
  branch TEXT NOT NULL DEFAULT '',
  commit_hash TEXT NOT NULL DEFAULT '',
  message_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  ingested_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  source_fingerprint TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (repo_id) REFERENCES repositories(id)
);

CREATE TABLE session_events (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  type TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  timestamp TEXT NOT NULL,
  structured_value TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL,
  title TEXT NOT NULL,
  goal TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT '',
  pinned INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_active_at TEXT NOT NULL,
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

CREATE TABLE working_states (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  compiled_at TEXT NOT NULL,
  source_watermark TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE auth_tokens (
  token TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_repositories_root_path ON repositories(root_path);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_agent_source ON sessions(agent, source_path);
CREATE INDEX IF NOT EXISTS idx_sessions_repo_agent ON sessions(repo_id, agent);
CREATE INDEX IF NOT EXISTS idx_sessions_repo_status ON sessions(repo_id, status);
CREATE INDEX IF NOT EXISTS idx_session_events_session ON session_events(session_id, sequence);
CREATE INDEX IF NOT EXISTS idx_tasks_repo_status ON tasks(repo_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_last_active ON tasks(last_active_at);
CREATE INDEX IF NOT EXISTS idx_task_sessions_session ON task_sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_working_states_task ON working_states(task_id, version);
CREATE INDEX IF NOT EXISTS idx_auth_tokens_user ON auth_tokens(user_id);
