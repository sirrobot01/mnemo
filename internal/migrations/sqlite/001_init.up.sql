CREATE TABLE repositories (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL,
  remote_url TEXT NOT NULL DEFAULT '',
  default_branch TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE memories (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL,
  type TEXT NOT NULL,
  scope TEXT NOT NULL,
  content TEXT NOT NULL,
  structured_value TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL,
  confidence REAL NOT NULL,
  priority REAL NOT NULL,
  source TEXT NOT NULL,
  valid_from TEXT NOT NULL,
  valid_until TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_verified_at TEXT,
  created_by TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (repo_id) REFERENCES repositories(id)
);

CREATE TABLE memory_evidence (
  id TEXT PRIMARY KEY,
  memory_id TEXT NOT NULL,
  type TEXT NOT NULL,
  path TEXT NOT NULL DEFAULT '',
  commit_hash TEXT NOT NULL DEFAULT '',
  line_start INTEGER NOT NULL DEFAULT 0,
  line_end INTEGER NOT NULL DEFAULT 0,
  reason TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY (memory_id) REFERENCES memories(id)
);

CREATE TABLE memory_tags (
  id TEXT PRIMARY KEY,
  memory_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (memory_id) REFERENCES memories(id)
);

CREATE TABLE proposals (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL,
  type TEXT NOT NULL,
  scope TEXT NOT NULL,
  content TEXT NOT NULL,
  structured_value TEXT NOT NULL DEFAULT '{}',
  confidence REAL NOT NULL,
  evidence TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (repo_id) REFERENCES repositories(id)
);

CREATE TABLE conflicts (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL,
  memory_id_a TEXT NOT NULL,
  memory_id_b TEXT NOT NULL,
  type TEXT NOT NULL,
  description TEXT NOT NULL,
  suggested_resolution TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (repo_id) REFERENCES repositories(id)
);

CREATE TABLE projections (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL,
  tool TEXT NOT NULL,
  path TEXT NOT NULL,
  template TEXT NOT NULL,
  checksum TEXT NOT NULL DEFAULT '',
  managed_mode TEXT NOT NULL,
  last_generated_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (repo_id) REFERENCES repositories(id)
);

CREATE TABLE events (
  id TEXT PRIMARY KEY,
  repo_id TEXT NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '{}',
  source TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (repo_id) REFERENCES repositories(id)
);

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE settings (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
