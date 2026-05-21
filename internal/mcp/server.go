// Package mcp exposes Mnemo's continuity surface to MCP-aware agents over
// stdio. It is a second transport adapter over the same services the CLI and
// HTTP API use — no business logic lives here. Scope is read + ingest:
// resume, list tasks, fetch a task's compiled state, and trigger a sweep.
// Task mutation (start/switch/pause/done) is intentionally not exposed.
package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

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

// Stores is the composite both backend adapters satisfy — identical to the
// HTTP API's, so the MCP surface stays behaviourally consistent with it.
type Stores interface {
	storage.RepositoryStore
	storage.SessionStore
	storage.TaskStore
	storage.WorkingStateStore
}

// Server binds the MCP tool surface to one repository's stores.
type Server struct {
	repo     domain.Repository
	store    Stores
	registry *sessions.Registry
	cfg      config.Config
	mcp      *sdk.Server
}

// New builds the MCP server for a repository. cfg is read once from the
// project's config file (the same path the HTTP API's states() reads) so
// enrichment and the cross-vendor-egress default match `mnemo resume`.
func New(repo domain.Repository, store Stores, registry *sessions.Registry, version string) (*Server, error) {
	cfg, err := loadProjectConfig(repo.RootPath)
	if err != nil {
		return nil, err
	}
	s := &Server{repo: repo, store: store, registry: registry, cfg: cfg}

	srv := sdk.NewServer(&sdk.Implementation{Name: "mnemo", Version: version}, nil)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "mnemo_resume",
		Description: "Compile and return the state of play for a task (defaults to the most-recently-active task). This is the continuity handoff an agent should read at the start of a session.",
	}, s.resume)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "mnemo_list_tasks",
		Description: "List the repository's tasks (active, paused, done) after threading any freshly ingested sessions.",
	}, s.listTasks)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "mnemo_task_state",
		Description: "Fetch the latest compiled working state (state of play) for a specific task id, without re-rendering it for an agent.",
	}, s.taskState)
	sdk.AddTool(srv, &sdk.Tool{
		Name:        "mnemo_ingest",
		Description: "Sweep every configured agent's session directory, ingest new/changed transcripts, and re-thread tasks. Returns a per-agent summary.",
	}, s.ingest)
	s.mcp = srv
	return s, nil
}

// Run serves the MCP protocol over stdio until the client disconnects or the
// context is cancelled. stdout carries the JSON-RPC stream; callers must keep
// it clean (logs go to stderr).
func (s *Server) Run(ctx context.Context) error {
	return s.mcp.Run(ctx, &sdk.StdioTransport{})
}

// HTTPHandler returns the Streamable HTTP transport for this server (the
// current MCP remote transport; the older HTTP+SSE transport is deprecated).
// The same wrapped *sdk.Server is reused across sessions, which the SDK
// explicitly permits.
//
// If auth is non-nil, every request except GET /healthz must carry a valid
// bearer token. Tokens are the same ones `mnemo serve` issues — both share
// the repository's auth store — so there is no separate login endpoint here.
func (s *Server) HTTPHandler(auth *authsvc.Service) http.Handler {
	streamable := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s.mcp }, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", streamable)
	if auth == nil {
		return mux
	}
	return bearerAuth(auth, mux)
}

// bearerAuth mirrors the HTTP API's gate: GET /healthz is open, everything
// else requires a valid bearer token.
func bearerAuth(auth *authsvc.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		tok := strings.TrimPrefix(header, "Bearer ")
		if tok == "" || tok == header {
			http.Error(w, `{"error":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		if _, err := auth.Authenticate(r.Context(), tok); err != nil {
			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loadProjectConfig mirrors api.Server.states(): a missing config file is not
// an error (zero config), it just means no enrichment / default privacy.
func loadProjectConfig(root string) (config.Config, error) {
	path := config.DefaultPath(root)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return config.Config{}, nil
	}
	return config.Load(path)
}

func (s *Server) tasks() *tasksvc.Service {
	return tasksvc.New(s.repo, s.store, s.store, s.store, tasksvc.DefaultIdleWindow)
}

func (s *Server) states() (*statesvc.Service, error) {
	svc := statesvc.New(s.store, s.store, s.store)
	enricher, err := enrich.New(s.cfg.Enrichment)
	if err != nil {
		return nil, err
	}
	if enricher != nil {
		svc.SetEnricher(enricher)
	}
	return svc, nil
}

// sourceKinds is the set of distinct vendor kinds a task's sessions came
// from; resumesvc gates egress to a different vendor against it.
func (s *Server) sourceKinds(ctx context.Context, t *tasksvc.Service, taskID domain.ID) []domain.SessionKind {
	ids, _ := t.Sessions(ctx, taskID)
	seen := map[domain.SessionKind]bool{}
	kinds := []domain.SessionKind{}
	for _, id := range ids {
		if sess, err := s.store.GetSession(ctx, id); err == nil && !seen[sess.Kind] {
			seen[sess.Kind] = true
			kinds = append(kinds, sess.Kind)
		}
	}
	return kinds
}

type emptyInput struct{}

type resumeInput struct {
	TaskID           string `json:"task_id,omitempty" jsonschema:"task id to resume; defaults to the most-recently-active task"`
	Tool             string `json:"tool,omitempty" jsonschema:"the agent/vendor this state will be injected into (e.g. claude, codex); used for the cross-vendor egress check"`
	AllowCrossVendor bool   `json:"allow_cross_vendor,omitempty" jsonschema:"permit injecting into a different vendor's agent than the task's sessions came from"`
}

type resumeOutput struct {
	TaskID  string `json:"task_id"`
	Version int    `json:"version"`
	Tool    string `json:"tool"`
	Content string `json:"content"`
}

func (s *Server) resume(ctx context.Context, _ *sdk.CallToolRequest, in resumeInput) (*sdk.CallToolResult, resumeOutput, error) {
	t := s.tasks()
	if _, err := t.Thread(ctx); err != nil {
		return nil, resumeOutput{}, err
	}

	var task domain.Task
	var err error
	if strings.TrimSpace(in.TaskID) != "" {
		task, err = t.Get(ctx, domain.ID(in.TaskID))
	} else {
		var ok bool
		task, ok, err = t.Active(ctx)
		if err == nil && !ok {
			err = tasksvc.ErrNoActiveTask
		}
	}
	if err != nil {
		return nil, resumeOutput{}, err
	}

	ssvc, err := s.states()
	if err != nil {
		return nil, resumeOutput{}, err
	}
	ws, err := ssvc.Compile(ctx, task.ID)
	if err != nil {
		return nil, resumeOutput{}, err
	}

	allowed := in.AllowCrossVendor || s.cfg.Privacy.AllowCrossVendorEgress
	rendered, err := resumesvc.Render(ws, s.sourceKinds(ctx, t, task.ID), resumesvc.Options{
		Tool:               in.Tool,
		CrossVendorAllowed: allowed,
	})
	if err != nil {
		return nil, resumeOutput{}, err
	}
	return nil, resumeOutput{
		TaskID:  string(task.ID),
		Version: ws.Version,
		Tool:    rendered.Tool,
		Content: rendered.Content,
	}, nil
}

type listTasksOutput struct {
	Tasks []domain.Task `json:"tasks"`
}

func (s *Server) listTasks(ctx context.Context, _ *sdk.CallToolRequest, _ emptyInput) (*sdk.CallToolResult, listTasksOutput, error) {
	t := s.tasks()
	if _, err := t.Thread(ctx); err != nil {
		return nil, listTasksOutput{}, err
	}
	tasks, err := t.List(ctx)
	if err != nil {
		return nil, listTasksOutput{}, err
	}
	return nil, listTasksOutput{Tasks: tasks}, nil
}

type taskStateInput struct {
	TaskID string `json:"task_id" jsonschema:"task id whose latest compiled working state to fetch"`
}

func (s *Server) taskState(ctx context.Context, _ *sdk.CallToolRequest, in taskStateInput) (*sdk.CallToolResult, domain.WorkingState, error) {
	if strings.TrimSpace(in.TaskID) == "" {
		return nil, domain.WorkingState{}, fmt.Errorf("task_id is required")
	}
	ws, err := s.store.GetLatestWorkingState(ctx, domain.ID(in.TaskID))
	if err != nil {
		return nil, domain.WorkingState{}, err
	}
	return nil, ws, nil
}

type ingestOutput struct {
	Results []ingestsvc.ImportResult `json:"results"`
}

func (s *Server) ingest(ctx context.Context, _ *sdk.CallToolRequest, _ emptyInput) (*sdk.CallToolResult, ingestOutput, error) {
	isvc := ingestsvc.New(s.repo, s.store, s.registry)
	results, err := isvc.Import(ctx)
	if err != nil {
		return nil, ingestOutput{}, err
	}
	if _, err := s.tasks().Thread(ctx); err != nil {
		return nil, ingestOutput{}, err
	}
	return nil, ingestOutput{Results: results}, nil
}
