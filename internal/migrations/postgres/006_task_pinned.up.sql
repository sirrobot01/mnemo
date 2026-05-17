-- A pinned task is the explicitly chosen active task (`mnemo task start` /
-- `task switch`). It captures newly-ingested sessions regardless of the
-- branch heuristic — "explicit override always wins".
ALTER TABLE tasks ADD COLUMN pinned BOOLEAN NOT NULL DEFAULT false;
