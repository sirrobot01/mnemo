// Package enrich provides the optional HTTP-backed implementation of
// statesvc.Enricher. It is disabled by default and only runs when configured.
package enrich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/config"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/safety"
)

const (
	providerOpenAI    = "openai"
	providerAnthropic = "anthropic"
	providerOllama    = "ollama"

	defaultTimeout         = 20 * time.Second
	defaultMaxEvents       = 80
	defaultMaxEventChars   = 2400
	defaultMaxInputChars   = 60000
	defaultMaxOutputTokens = 1600
)

type providerDefaults struct {
	kind           string
	baseURL        string
	apiKeyEnv      string
	apiKeyRequired bool
}

// Service refines a deterministic WorkingState by asking a configured model
// to return a strict JSON patch for the state-of-play fields.
type Service struct {
	provider        string
	baseURL         string
	model           string
	apiKey          string
	client          *http.Client
	timeout         time.Duration
	maxEvents       int
	maxEventChars   int
	maxInputChars   int
	maxOutputTokens int
	temperature     float64
}

// New returns nil when enrichment is disabled. When enabled, it validates the
// configured provider and endpoint before a compile ever tries to call it.
func New(cfg config.EnrichmentConfig) (*Service, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	defaults, err := defaultsForProvider(cfg.Provider)
	if err != nil {
		return nil, err
	}
	provider := defaults.kind
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, errors.New("enrichment.model is required when enrichment is enabled")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaults.baseURL
	}
	if baseURL == "" {
		return nil, fmt.Errorf("enrichment.base_url is required for provider %q", cfg.Provider)
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid enrichment.base_url: %w", err)
	}

	apiKeyEnv := strings.TrimSpace(cfg.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = defaults.apiKeyEnv
	}
	apiKey := ""
	if apiKeyEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(apiKeyEnv))
	}
	if defaults.apiKeyRequired && apiKey == "" {
		return nil, fmt.Errorf("enrichment.api_key_env %q is not set", apiKeyEnv)
	}

	timeout := defaultTimeout
	if cfg.Timeout != "" {
		parsed, err := time.ParseDuration(cfg.Timeout)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("invalid enrichment.timeout %q", cfg.Timeout)
		}
		timeout = parsed
	}

	return &Service{
		provider:        provider,
		baseURL:         strings.TrimRight(baseURL, "/"),
		model:           model,
		apiKey:          apiKey,
		client:          &http.Client{Timeout: timeout},
		timeout:         timeout,
		maxEvents:       positiveOr(cfg.MaxEvents, defaultMaxEvents),
		maxEventChars:   positiveOr(cfg.MaxEventChars, defaultMaxEventChars),
		maxInputChars:   positiveOr(cfg.MaxInputChars, defaultMaxInputChars),
		maxOutputTokens: positiveOr(cfg.MaxOutputTokens, defaultMaxOutputTokens),
		temperature:     cfg.Temperature,
	}, nil
}

func defaultsForProvider(provider string) (providerDefaults, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return providerDefaults{
			kind: providerOpenAI, baseURL: "https://api.openai.com/v1",
			apiKeyEnv: "OPENAI_API_KEY", apiKeyRequired: true,
		}, nil
	case "anthropic", "claude":
		return providerDefaults{
			kind: providerAnthropic, baseURL: "https://api.anthropic.com/v1",
			apiKeyEnv: "ANTHROPIC_API_KEY", apiKeyRequired: true,
		}, nil
	case "openai_compatible", "openai-compatible", "compatible":
		return providerDefaults{kind: providerOpenAI}, nil
	case "openrouter":
		return providerDefaults{
			kind: providerOpenAI, baseURL: "https://openrouter.ai/api/v1",
			apiKeyEnv: "OPENROUTER_API_KEY", apiKeyRequired: true,
		}, nil
	case "lmstudio", "lm-studio":
		return providerDefaults{kind: providerOpenAI, baseURL: "http://localhost:1234/v1"}, nil
	case "localai":
		return providerDefaults{kind: providerOpenAI, baseURL: "http://localhost:8080/v1"}, nil
	case "ollama":
		return providerDefaults{kind: providerOllama, baseURL: "http://localhost:11434"}, nil
	default:
		return providerDefaults{}, fmt.Errorf("unsupported enrichment.provider %q (supported: openai, anthropic, openai_compatible, openrouter, lmstudio, localai, ollama)", provider)
	}
}

func positiveOr(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// Enrich implements statesvc.Enricher.
func (s *Service) Enrich(ctx context.Context, state domain.WorkingState, events []domain.SessionEvent) (domain.WorkingState, error) {
	system, user := s.prompt(state, events)

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	var raw string
	var err error
	switch s.provider {
	case providerOpenAI:
		raw, err = s.openai(ctx, system, user)
	case providerAnthropic:
		raw, err = s.anthropic(ctx, system, user)
	case providerOllama:
		raw, err = s.ollama(ctx, system, user)
	default:
		err = fmt.Errorf("unsupported enrichment provider %q", s.provider)
	}
	if err != nil {
		return domain.WorkingState{}, err
	}

	patch, err := parsePatch(raw)
	if err != nil {
		return domain.WorkingState{}, err
	}
	return applyPatch(state, patch), nil
}

func (s *Service) openai(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": s.temperature,
		"stream":      false,
	}
	if s.maxOutputTokens > 0 {
		body["max_tokens"] = s.maxOutputTokens
	}
	headers := map[string]string{}
	if s.apiKey != "" {
		headers["Authorization"] = "Bearer " + s.apiKey
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := s.postJSON(ctx, s.endpoint("/chat/completions"), headers, body, &response); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", errors.New("enrichment provider returned no content")
	}
	return response.Choices[0].Message.Content, nil
}

func (s *Service) anthropic(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model":       s.model,
		"system":      system,
		"temperature": s.temperature,
		"max_tokens":  s.maxOutputTokens,
		"messages": []map[string]string{
			{"role": "user", "content": user},
		},
	}
	headers := map[string]string{
		"anthropic-version": "2023-06-01",
	}
	if s.apiKey != "" {
		headers["x-api-key"] = s.apiKey
	}

	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := s.postJSON(ctx, s.endpoint("/messages"), headers, body, &response); err != nil {
		return "", err
	}
	var out strings.Builder
	for _, part := range response.Content {
		if part.Type == "" || part.Type == "text" {
			out.WriteString(part.Text)
		}
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", errors.New("enrichment provider returned no content")
	}
	return out.String(), nil
}

func (s *Service) ollama(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model":  s.model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"options": map[string]any{"temperature": s.temperature},
	}
	var response struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Response string `json:"response"`
	}
	if err := s.postJSON(ctx, s.endpoint("/api/chat"), nil, body, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Message.Content) != "" {
		return response.Message.Content, nil
	}
	if strings.TrimSpace(response.Response) != "" {
		return response.Response, nil
	}
	return "", errors.New("enrichment provider returned no content")
}

func (s *Service) endpoint(path string) string {
	return strings.TrimRight(s.baseURL, "/") + path
}

func (s *Service) postJSON(ctx context.Context, endpoint string, headers map[string]string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("enrichment provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode enrichment response: %w", err)
	}
	return nil
}

const systemPrompt = `You refine Mnemo's deterministic state-of-play for a coding task.
Return only a JSON object. Do not include markdown fences or commentary.
Do not invent work. Prefer concise, operational bullets grounded in the events.
Keep uncertainty in hypotheses with confirmed=false. Do not include secrets.`

func (s *Service) prompt(state domain.WorkingState, events []domain.SessionEvent) (string, string) {
	payload := map[string]any{
		"current_state": sanitizeStateForPrompt(state),
		"events":        s.promptEvents(events),
		"output_schema": map[string]any{
			"goal":           "string",
			"done":           []string{},
			"in_progress":    "string",
			"next_steps":     []string{},
			"rejected":       []domain.RejectedApproach{},
			"decisions":      []domain.Decision{},
			"open_questions": []string{},
			"files_touched":  []domain.FileTouched{},
			"hypotheses":     []domain.Hypothesis{},
		},
	}
	encoded, _ := json.MarshalIndent(payload, "", "  ")
	for len(encoded) > s.maxInputChars && len(payload["events"].([]promptEvent)) > 0 {
		evs := payload["events"].([]promptEvent)
		payload["events"] = evs[1:]
		encoded, _ = json.MarshalIndent(payload, "", "  ")
	}
	user := "Refine the current_state using the session events. Return the output_schema shape as concrete JSON.\n" + string(encoded)
	if len(user) > s.maxInputChars {
		user = user[:s.maxInputChars]
	}
	return systemPrompt, user
}

type promptEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	Content   string `json:"content"`
}

func (s *Service) promptEvents(events []domain.SessionEvent) []promptEvent {
	start := 0
	if len(events) > s.maxEvents {
		start = len(events) - s.maxEvents
	}
	out := make([]promptEvent, 0, len(events)-start)
	for _, event := range events[start:] {
		content := cleanForPrompt(event.Content, s.maxEventChars)
		if content == "" {
			continue
		}
		out = append(out, promptEvent{
			Type:      string(event.Type),
			Timestamp: event.Timestamp.UTC().Format(time.RFC3339),
			Content:   content,
		})
	}
	return out
}

func sanitizeStateForPrompt(state domain.WorkingState) domain.WorkingState {
	state.ID = ""
	state.TaskID = ""
	state.Version = 0
	state.CompiledAt = time.Time{}
	state.CreatedAt = time.Time{}
	state.SourceWatermark = ""
	state.Goal = cleanForPrompt(state.Goal, 600)
	state.InProgress = cleanForPrompt(state.InProgress, 800)
	state.Done = cleanStringSliceForPrompt(state.Done, 300)
	state.NextSteps = cleanStringSliceForPrompt(state.NextSteps, 300)
	state.OpenQuestions = cleanStringSliceForPrompt(state.OpenQuestions, 300)
	for i := range state.Rejected {
		state.Rejected[i].Approach = cleanForPrompt(state.Rejected[i].Approach, 300)
		state.Rejected[i].Reason = cleanForPrompt(state.Rejected[i].Reason, 300)
	}
	for i := range state.Decisions {
		state.Decisions[i].Decision = cleanForPrompt(state.Decisions[i].Decision, 300)
		state.Decisions[i].Rationale = cleanForPrompt(state.Decisions[i].Rationale, 300)
	}
	for i := range state.FilesTouched {
		state.FilesTouched[i].Path = cleanForPrompt(state.FilesTouched[i].Path, 300)
		state.FilesTouched[i].Summary = cleanForPrompt(state.FilesTouched[i].Summary, 300)
	}
	for i := range state.Hypotheses {
		state.Hypotheses[i].Claim = cleanForPrompt(state.Hypotheses[i].Claim, 300)
	}
	return state
}

func cleanStringSliceForPrompt(values []string, max int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if cleaned := cleanForPrompt(value, max); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

type statePatch struct {
	Goal          *string                    `json:"goal"`
	Done          *[]string                  `json:"done"`
	InProgress    *string                    `json:"in_progress"`
	NextSteps     *[]string                  `json:"next_steps"`
	Rejected      *[]domain.RejectedApproach `json:"rejected"`
	Decisions     *[]domain.Decision         `json:"decisions"`
	OpenQuestions *[]string                  `json:"open_questions"`
	FilesTouched  *[]domain.FileTouched      `json:"files_touched"`
	Hypotheses    *[]domain.Hypothesis       `json:"hypotheses"`
}

func parsePatch(raw string) (statePatch, error) {
	var patch statePatch
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return patch, errors.New("enrichment response did not contain a JSON object")
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &patch); err != nil {
		return patch, fmt.Errorf("decode enrichment JSON: %w", err)
	}
	return patch, nil
}

func applyPatch(state domain.WorkingState, patch statePatch) domain.WorkingState {
	if patch.Goal != nil {
		if goal := cleanOutput(*patch.Goal, 240); goal != "" {
			state.Goal = goal
		}
	}
	if patch.InProgress != nil {
		state.InProgress = cleanOutput(*patch.InProgress, 300)
	}
	if patch.Done != nil {
		state.Done = cleanOutputList(*patch.Done, 12, 220)
	}
	if patch.NextSteps != nil {
		state.NextSteps = cleanOutputList(*patch.NextSteps, 12, 220)
	}
	if patch.OpenQuestions != nil {
		state.OpenQuestions = cleanOutputList(*patch.OpenQuestions, 12, 220)
	}
	if patch.Rejected != nil {
		state.Rejected = cleanRejected(*patch.Rejected)
	}
	if patch.Decisions != nil {
		state.Decisions = cleanDecisions(*patch.Decisions)
	}
	if patch.FilesTouched != nil {
		state.FilesTouched = cleanFiles(*patch.FilesTouched)
	}
	if patch.Hypotheses != nil {
		state.Hypotheses = cleanHypotheses(*patch.Hypotheses)
	}
	return state
}

func cleanRejected(values []domain.RejectedApproach) []domain.RejectedApproach {
	out := make([]domain.RejectedApproach, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		approach := cleanOutput(value.Approach, 220)
		if approach == "" || seen[strings.ToLower(approach)] {
			continue
		}
		seen[strings.ToLower(approach)] = true
		out = append(out, domain.RejectedApproach{Approach: approach, Reason: cleanOutput(value.Reason, 220)})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func cleanDecisions(values []domain.Decision) []domain.Decision {
	out := make([]domain.Decision, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		decision := cleanOutput(value.Decision, 220)
		if decision == "" || seen[strings.ToLower(decision)] {
			continue
		}
		seen[strings.ToLower(decision)] = true
		out = append(out, domain.Decision{Decision: decision, Rationale: cleanOutput(value.Rationale, 220)})
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func cleanFiles(values []domain.FileTouched) []domain.FileTouched {
	out := make([]domain.FileTouched, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		path := cleanOutput(value.Path, 260)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, domain.FileTouched{Path: path, Summary: cleanOutput(value.Summary, 220)})
		if len(out) >= 25 {
			break
		}
	}
	return out
}

func cleanHypotheses(values []domain.Hypothesis) []domain.Hypothesis {
	out := make([]domain.Hypothesis, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		claim := cleanOutput(value.Claim, 240)
		if claim == "" || seen[strings.ToLower(claim)] {
			continue
		}
		seen[strings.ToLower(claim)] = true
		if value.Confidence < 0 {
			value.Confidence = 0
		}
		if value.Confidence > 1 {
			value.Confidence = 1
		}
		out = append(out, domain.Hypothesis{Claim: claim, Confidence: value.Confidence, Confirmed: value.Confirmed})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func cleanOutputList(values []string, limit int, max int) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		cleaned := cleanOutput(value, max)
		key := strings.ToLower(cleaned)
		if cleaned == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, cleaned)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func cleanOutput(value string, max int) string {
	cleaned := cleanForPrompt(value, max)
	if strings.Contains(cleaned, "[REDACTED]") {
		return ""
	}
	return cleaned
}

func cleanForPrompt(value string, max int) string {
	value = strings.TrimSpace(redactSecrets(value))
	if max > 0 && len(value) > max {
		value = strings.TrimSpace(value[:max])
	}
	return value
}

func redactSecrets(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if safety.RejectIfSecret(line) != nil {
			lines[i] = "[REDACTED]"
		}
	}
	return strings.Join(lines, "\n")
}
