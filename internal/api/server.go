// Package api exposes the local HTTP surface for the continuity product:
// tasks, their state of play, sessions/events, ingest, and resume. It binds
// to localhost only (see cmd wiring) and shares the same services the CLI
// uses.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sirrobot01/mnemo/internal/app/ingestsvc"
	"github.com/sirrobot01/mnemo/internal/app/resumesvc"
	"github.com/sirrobot01/mnemo/internal/app/statesvc"
	"github.com/sirrobot01/mnemo/internal/app/tasksvc"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/sessions"
	"github.com/sirrobot01/mnemo/internal/storage"
)

// Stores is the composite both backend adapters satisfy.
type Stores interface {
	storage.RepositoryStore
	storage.SessionStore
	storage.TaskStore
	storage.WorkingStateStore
}

// DBStatusFunc reports migration status for the /v1/db/status route.
type DBStatusFunc func() (backend string, applied, pending int, err error)

type Server struct {
	repo     domain.Repository
	store    Stores
	adapters []sessions.Adapter
	dbStatus DBStatusFunc
}

func New(repo domain.Repository, store Stores, adapters []sessions.Adapter, dbStatus DBStatusFunc) http.Handler {
	s := &Server{repo: repo, store: store, adapters: adapters, dbStatus: dbStatus}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("GET /v1/db/status", s.dbStatusHandler)
	mux.HandleFunc("GET /v1/tasks", s.listTasks)
	mux.HandleFunc("GET /v1/tasks/{id}", s.getTask)
	mux.HandleFunc("GET /v1/tasks/{id}/state", s.taskState)
	mux.HandleFunc("GET /v1/tasks/{id}/sessions", s.taskSessions)
	mux.HandleFunc("GET /v1/sessions/{id}/events", s.sessionEvents)
	mux.HandleFunc("POST /v1/ingest", s.ingest)
	mux.HandleFunc("POST /v1/resume", s.resume)
	mux.HandleFunc("DELETE /v1/sessions/{id}", s.deleteSession)
	return mux
}

func (s *Server) tasks() *tasksvc.Service {
	return tasksvc.New(s.repo, s.store, s.store, s.store, tasksvc.DefaultIdleWindow)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, tasksvc.ErrNoActiveTask):
		code = http.StatusNotFound
	case errors.Is(err, resumesvc.ErrCrossVendorEgress):
		code = http.StatusForbidden
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repository": s.repo.ID})
}

func (s *Server) dbStatusHandler(w http.ResponseWriter, _ *http.Request) {
	if s.dbStatus == nil {
		writeJSON(w, http.StatusOK, map[string]any{"backend": "unknown"})
		return
	}
	backend, applied, pending, err := s.dbStatus()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backend": backend, "applied": applied, "pending": pending})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	t := s.tasks()
	if _, err := t.Thread(r.Context()); err != nil {
		s.fail(w, err)
		return
	}
	tasks, err := t.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.tasks().Get(r.Context(), domain.ID(r.PathValue("id")))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) taskState(w http.ResponseWriter, r *http.Request) {
	ws, err := s.store.GetLatestWorkingState(r.Context(), domain.ID(r.PathValue("id")))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (s *Server) taskSessions(w http.ResponseWriter, r *http.Request) {
	ids, err := s.tasks().Sessions(r.Context(), domain.ID(r.PathValue("id")))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ids)
}

func (s *Server) sessionEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListSessionEvents(r.Context(), storage.SessionEventFilter{SessionID: domain.ID(r.PathValue("id"))})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	isvc := ingestsvc.New(s.repo, s.store, s.adapters...)
	results, err := isvc.Import(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	if _, err := s.tasks().Thread(r.Context()); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

type resumeRequest struct {
	TaskID           string `json:"task_id"`
	Tool             string `json:"tool"`
	AllowCrossVendor bool   `json:"allow_cross_vendor"`
}

func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	var req resumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	t := s.tasks()
	if _, err := t.Thread(r.Context()); err != nil {
		s.fail(w, err)
		return
	}
	var task domain.Task
	var err error
	if req.TaskID != "" {
		task, err = t.Get(r.Context(), domain.ID(req.TaskID))
	} else {
		var ok bool
		task, ok, err = t.Active(r.Context())
		if err == nil && !ok {
			err = tasksvc.ErrNoActiveTask
		}
	}
	if err != nil {
		s.fail(w, err)
		return
	}

	ws, err := statesvc.New(s.store, s.store, s.store).Compile(r.Context(), task.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	ids, _ := t.Sessions(r.Context(), task.ID)
	seen := map[domain.SessionTool]bool{}
	tools := []domain.SessionTool{}
	for _, id := range ids {
		if sess, err := s.store.GetSession(r.Context(), id); err == nil && !seen[sess.Tool] {
			seen[sess.Tool] = true
			tools = append(tools, sess.Tool)
		}
	}
	rendered, err := resumesvc.Render(ws, tools, resumesvc.Options{Tool: req.Tool, CrossVendorAllowed: req.AllowCrossVendor})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rendered)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	sid := domain.ID(r.PathValue("id"))
	tasks, err := s.store.ListTasks(r.Context(), storage.TaskFilter{RepoID: s.repo.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, tk := range tasks {
		ids, err := s.store.ListTaskSessions(r.Context(), tk.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		for _, id := range ids {
			if id == sid {
				if err := s.store.DetachSession(r.Context(), tk.ID, sid); err != nil {
					s.fail(w, err)
					return
				}
			}
		}
	}
	if err := s.store.DeleteSession(r.Context(), sid); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"forgot": string(sid)})
}
