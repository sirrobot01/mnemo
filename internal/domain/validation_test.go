package domain

import "testing"

func TestScopeValid(t *testing.T) {
	for _, s := range []Scope{ScopeGlobal, ScopeUser, ScopeRepository} {
		if !s.Valid() {
			t.Errorf("expected %q valid", s)
		}
	}
	if Scope("team").Valid() {
		t.Error("expected removed scope invalid")
	}
}

func TestSessionValidate(t *testing.T) {
	s := Session{ID: "sess_1", RepoID: "repo_1", Agent: "claude", Kind: SessionKindClaude, SourcePath: "/x.jsonl", Status: SessionStatusIngested}
	if err := s.Validate(); err != nil {
		t.Fatalf("valid session rejected: %v", err)
	}
	if (Session{}).Validate() == nil {
		t.Error("empty session should be invalid")
	}
}

func TestSessionEventValidate(t *testing.T) {
	e := SessionEvent{ID: "sev_1", SessionID: "sess_1", Type: SessionEventTypeUserMessage, Sequence: 0}
	if err := e.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	bad := SessionEvent{ID: "sev_2", SessionID: "sess_1", Type: SessionEventTypeUserMessage, Sequence: -1}
	if bad.Validate() == nil {
		t.Error("negative sequence should be invalid")
	}
}

func TestTaskValidateAndTransitions(t *testing.T) {
	task := Task{ID: "task_1", RepoID: "repo_1", Title: "fix race", Status: TaskStatusActive}
	if err := task.Validate(); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}
	if (Task{ID: "task_2", RepoID: "repo_1", Status: TaskStatusActive}).Validate() == nil {
		t.Error("task without title should be invalid")
	}
	if !TaskStatusActive.CanTransitionTo(TaskStatusPaused) {
		t.Error("active -> paused should be allowed")
	}
	if !TaskStatusPaused.CanTransitionTo(TaskStatusActive) {
		t.Error("paused -> active should be allowed")
	}
	if !TaskStatusActive.CanTransitionTo(TaskStatusDone) {
		t.Error("active -> done should be allowed")
	}
	if TaskStatusDone.CanTransitionTo(TaskStatusActive) {
		t.Error("done is terminal")
	}
}

func TestWorkingStateValidate(t *testing.T) {
	ws := WorkingState{ID: "ws_1", TaskID: "task_1", Version: 1}
	if err := ws.Validate(); err != nil {
		t.Fatalf("valid working state rejected: %v", err)
	}
	if (WorkingState{ID: "ws_2", TaskID: "task_1", Version: 0}).Validate() == nil {
		t.Error("version < 1 should be invalid")
	}
}
