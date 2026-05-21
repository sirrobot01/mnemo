// Package api exposes the local HTTP surface for the continuity product:
// tasks, their state of play, sessions/events, ingest, and resume. It binds
// to localhost only (see cmd wiring) and shares the same services the CLI
// uses.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/sirrobot01/mnemo/internal/app/authsvc"
	"github.com/sirrobot01/mnemo/internal/app/enrich"
	"github.com/sirrobot01/mnemo/internal/app/ingestsvc"
	"github.com/sirrobot01/mnemo/internal/app/resumesvc"
	"github.com/sirrobot01/mnemo/internal/app/statesvc"
	"github.com/sirrobot01/mnemo/internal/app/tasksvc"
	"github.com/sirrobot01/mnemo/internal/config"
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
	repo        domain.Repository
	store       Stores
	registry    *sessions.Registry
	dbStatus    DBStatusFunc
	auth        *authsvc.Service
	allowSignup bool
}

// New builds the API handler. auth is nil only when the caller disables
// gating; when non-nil every /v1 route except health and /v1/auth/* requires
// a valid bearer token.
func New(repo domain.Repository, store Stores, registry *sessions.Registry, dbStatus DBStatusFunc, auth *authsvc.Service, allowSignup bool) http.Handler {
	s := &Server{repo: repo, store: store, registry: registry, dbStatus: dbStatus, auth: auth, allowSignup: allowSignup}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	if auth != nil {
		mux.HandleFunc("POST /v1/auth/signup", s.signup)
		mux.HandleFunc("POST /v1/auth/login", s.login)
		mux.HandleFunc("POST /v1/auth/logout", s.logout)
	}
	mux.HandleFunc("GET /v1/db/status", s.dbStatusHandler)
	mux.HandleFunc("GET /v1/tasks", s.listTasks)
	mux.HandleFunc("POST /v1/tasks", s.startTask)
	mux.HandleFunc("GET /v1/tasks/{id}", s.getTask)
	mux.HandleFunc("DELETE /v1/tasks/{id}", s.forgetTask)
	mux.HandleFunc("GET /v1/tasks/{id}/state", s.taskState)
	mux.HandleFunc("GET /v1/tasks/{id}/sessions", s.taskSessions)
	mux.HandleFunc("POST /v1/tasks/{id}/switch", s.switchTask)
	mux.HandleFunc("POST /v1/tasks/{id}/pause", s.pauseTask)
	mux.HandleFunc("POST /v1/tasks/{id}/done", s.doneTask)
	mux.HandleFunc("GET /v1/sessions/{id}/events", s.sessionEvents)
	mux.HandleFunc("POST /v1/ingest", s.ingest)
	mux.HandleFunc("POST /v1/resume", s.resume)
	mux.HandleFunc("DELETE /v1/sessions/{id}", s.deleteSession)
	if auth != nil {
		return s.authMiddleware(mux)
	}
	return mux
}

// authMiddleware gates every request except health and the auth endpoints
// behind a Bearer token.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/v1/health" || strings.HasPrefix(p, "/v1/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		tok, ok := bearerToken(r)
		if !ok {
			http.Error(w, `{"error":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		if _, err := s.auth.Authenticate(r.Context(), tok); err != nil {
			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	tok := strings.TrimPrefix(header, "Bearer ")
	if tok == "" || tok == header {
		return "", false
	}
	return tok, true
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	if !s.allowSignup {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "signup is disabled"})
		return
	}
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	user, err := s.auth.Signup(r.Context(), req.Email, req.Password)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": string(user.ID), "email": user.Email})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	tok, err := s.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok.Token, "expires_at": tok.ExpiresAt})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	tok, ok := bearerToken(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
		return
	}
	if _, err := s.auth.Authenticate(r.Context(), tok); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired token"})
		return
	}
	if err := s.auth.Logout(r.Context(), tok); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "logout failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) tasks() *tasksvc.Service {
	return tasksvc.New(s.repo, s.store, s.store, s.store, tasksvc.DefaultIdleWindow)
}

func (s *Server) states() (*statesvc.Service, error) {
	svc := statesvc.New(s.store, s.store, s.store)
	path := config.DefaultPath(s.repo.RootPath)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return svc, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	enricher, err := enrich.New(cfg.Enrichment)
	if err != nil {
		return nil, err
	}
	if enricher != nil {
		svc.SetEnricher(enricher)
	}
	return svc, nil
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
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"repository":    s.repo.ID,
		"auth_required": s.auth != nil,
		"allow_signup":  s.auth != nil && s.allowSignup,
	})
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

type startTaskRequest struct {
	Title  string `json:"title"`
	Goal   string `json:"goal"`
	Branch string `json:"branch"`
}

func (s *Server) startTask(w http.ResponseWriter, r *http.Request) {
	var req startTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task title is required"})
		return
	}
	task, err := s.tasks().Start(r.Context(), req.Title, req.Goal, req.Branch)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.tasks().Get(r.Context(), domain.ID(r.PathValue("id")))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) forgetTask(w http.ResponseWriter, r *http.Request) {
	id := domain.ID(r.PathValue("id"))
	if err := s.tasks().ForgetTask(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"forgot": string(id)})
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

func (s *Server) switchTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.tasks().Switch(r.Context(), domain.ID(r.PathValue("id")))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) pauseTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.tasks().Pause(r.Context(), domain.ID(r.PathValue("id")))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) doneTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.tasks().Done(r.Context(), domain.ID(r.PathValue("id")))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
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
	isvc := ingestsvc.New(s.repo, s.store, s.registry)
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

	ssvc, err := s.states()
	if err != nil {
		s.fail(w, err)
		return
	}
	ws, err := ssvc.Compile(r.Context(), task.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	ids, _ := t.Sessions(r.Context(), task.ID)
	seen := map[domain.SessionKind]bool{}
	kinds := []domain.SessionKind{}
	for _, id := range ids {
		if sess, err := s.store.GetSession(r.Context(), id); err == nil && !seen[sess.Kind] {
			seen[sess.Kind] = true
			kinds = append(kinds, sess.Kind)
		}
	}
	rendered, err := resumesvc.Render(ws, kinds, resumesvc.Options{Tool: req.Tool, CrossVendorAllowed: req.AllowCrossVendor})
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
