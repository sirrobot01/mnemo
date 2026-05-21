export interface Task {
  id: string;
  repo_id: string;
  title: string;
  goal?: string;
  status: "active" | "paused" | "done";
  branch?: string;
  pinned: boolean;
  created_at: string;
  updated_at: string;
  last_active_at: string;
}

export type TaskAction = "switch" | "pause" | "done";

export interface RejectedApproach {
  approach: string;
  reason?: string;
}
export interface Decision {
  decision: string;
  rationale?: string;
}
export interface FileTouched {
  path: string;
  summary?: string;
}
export interface Hypothesis {
  claim: string;
  confidence?: number;
  confirmed: boolean;
}

export interface WorkingState {
  id: string;
  task_id: string;
  version: number;
  compiled_at: string;
  source_watermark?: string;
  goal?: string;
  done?: string[];
  in_progress?: string;
  next_steps?: string[];
  rejected?: RejectedApproach[];
  decisions?: Decision[];
  open_questions?: string[];
  files_touched?: FileTouched[];
  hypotheses?: Hypothesis[];
}

export interface SessionEvent {
  id: string;
  session_id: string;
  sequence: number;
  type: string;
  content: string;
  timestamp: string;
}

export interface IngestResult {
  agent: string;
  kind: string;
  discovered: number;
  imported: number;
  unchanged: number;
  skipped: number;
  redacted_events: number;
  redacted_sessions: number;
}

export interface Health {
  ok: boolean;
  repository: string;
  auth_required?: boolean;
  allow_signup?: boolean;
}

export interface DbStatus {
  backend?: string;
  applied?: number;
  pending?: number;
}

export interface CreateTaskRequest {
  title: string;
  goal?: string;
  branch?: string;
}

export interface ResumeRequest {
  task_id?: string;
  tool?: string;
  allow_cross_vendor?: boolean;
}

export interface RenderedResume {
  tool: string;
  content: string;
}

export interface AuthRequest {
  email: string;
  password: string;
}

export interface AuthLoginResponse {
  token: string;
  expires_at: string;
}
