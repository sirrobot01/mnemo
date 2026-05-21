CREATE TABLE repositories (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL,
  remote_url TEXT NOT NULL DEFAULT '',
  default_branch TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL REFERENCES repositories(id),
  agent TEXT NOT NULL,
  kind TEXT NOT NULL,
  source_path TEXT NOT NULL,
  external_id TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL,
  ended_at TIMESTAMPTZ,
  branch TEXT NOT NULL DEFAULT '',
  commit_hash TEXT NOT NULL DEFAULT '',
  message_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  ingested_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  source_fingerprint TEXT NOT NULL DEFAULT ''
);

CREATE TABLE session_events (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  sequence INTEGER NOT NULL,
  type TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  timestamp TIMESTAMPTZ NOT NULL,
  structured_value JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL REFERENCES repositories(id),
  title TEXT NOT NULL,
  goal TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT '',
  pinned BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  last_active_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE task_sessions (
  task_id TEXT NOT NULL REFERENCES tasks(id),
  session_id TEXT NOT NULL REFERENCES sessions(id),
  attached_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (task_id, session_id)
);

CREATE TABLE working_states (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id),
  version INTEGER NOT NULL,
  compiled_at TIMESTAMPTZ NOT NULL,
  source_watermark TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE auth_tokens (
  token TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id),
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
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
