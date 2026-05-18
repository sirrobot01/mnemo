-- Per-session fingerprint of the source transcript ("<modUnixNano>:<size>").
-- Ingestion skips re-parsing a file whose fingerprint is unchanged, which is
-- what keeps `mnemo watch` cheap on large/active transcripts.
ALTER TABLE sessions ADD COLUMN source_fingerprint TEXT NOT NULL DEFAULT '';
