package enrich

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/domain"
)

func TestOpenAICompatibleEnrichesWorkingState(t *testing.T) {
	t.Setenv("MNEMO_ENRICH_TEST_KEY", "test-key")
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "test-model" {
			t.Fatalf("model = %q", body.Model)
		}
		if len(body.Messages) != 2 || !strings.Contains(body.Messages[1].Content, "internal/auth/cache.go") {
			t.Fatalf("request did not include event payload: %+v", body.Messages)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"content": `{
					"goal": "fix auth cache race",
					"done": ["Implemented singleflight cache protection"],
					"in_progress": "Run regression tests",
					"next_steps": ["Add focused cache race test", "api_key: abcdefghijklmnop"],
					"rejected": [{"approach": "global mutex", "reason": "user rejected it"}],
					"decisions": [{"decision": "reuse existing cache helper"}],
					"open_questions": ["Which package owns the retry test?"],
					"files_touched": [{"path": "internal/auth/cache.go", "summary": "cache race fix"}],
					"hypotheses": [{"claim": "singleflight removes duplicate fetches", "confidence": 0.7, "confirmed": false}]
				}`},
			}},
		})
	}))
	defer server.Close()

	svc, err := New(config.EnrichmentConfig{
		Enabled:   true,
		Provider:  "openai_compatible",
		BaseURL:   server.URL + "/v1",
		Model:     "test-model",
		APIKeyEnv: "MNEMO_ENRICH_TEST_KEY",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	now := time.Now().UTC()
	got, err := svc.Enrich(context.Background(), domain.WorkingState{Goal: "old goal"}, []domain.SessionEvent{
		{Type: domain.SessionEventTypeUserMessage, Content: "fix race in internal/auth/cache.go", Timestamp: now},
	})
	if err != nil {
		t.Fatalf("Enrich returned error: %v", err)
	}
	if !sawRequest {
		t.Fatal("server did not receive request")
	}
	if got.Goal != "fix auth cache race" || len(got.Done) != 1 || got.InProgress != "Run regression tests" {
		t.Fatalf("unexpected enriched state: %+v", got)
	}
	if len(got.NextSteps) != 1 || got.NextSteps[0] != "Add focused cache race test" {
		t.Fatalf("secret-bearing next step should be dropped, got %+v", got.NextSteps)
	}
	if len(got.Rejected) != 1 || got.Rejected[0].Approach != "global mutex" {
		t.Fatalf("rejected not applied: %+v", got.Rejected)
	}
	if len(got.FilesTouched) != 1 || got.FilesTouched[0].Path != "internal/auth/cache.go" {
		t.Fatalf("files not applied: %+v", got.FilesTouched)
	}
}

func TestOllamaProviderUsesLocalChatEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "llama3.1" || body.Stream {
			t.Fatalf("unexpected request: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]string{"content": `{"goal":"local enriched goal","next_steps":["continue locally"]}`},
		})
	}))
	defer server.Close()

	svc, err := New(config.EnrichmentConfig{
		Enabled:  true,
		Provider: "ollama",
		BaseURL:  server.URL,
		Model:    "llama3.1",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	got, err := svc.Enrich(context.Background(), domain.WorkingState{Goal: "old"}, nil)
	if err != nil {
		t.Fatalf("Enrich returned error: %v", err)
	}
	if got.Goal != "local enriched goal" || len(got.NextSteps) != 1 {
		t.Fatalf("unexpected enriched state: %+v", got)
	}
}

func TestNewRequiresCloudAPIKey(t *testing.T) {
	_, err := New(config.EnrichmentConfig{Enabled: true, Provider: "openai", Model: "gpt-test"})
	if err == nil {
		t.Fatal("expected missing API key error")
	}
}
