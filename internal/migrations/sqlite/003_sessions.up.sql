CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL,
  tool TEXT NOT NULL,
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_tool_source ON sessions(tool, source_path);
CREATE INDEX IF NOT EXISTS idx_sessions_repo_tool ON sessions(repo_id, tool);
CREATE INDEX IF NOT EXISTS idx_sessions_repo_status ON sessions(repo_id, status);
CREATE INDEX IF NOT EXISTS idx_session_events_session ON session_events(session_id, sequence);
