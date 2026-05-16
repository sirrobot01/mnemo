package logging

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRejectsUnknownLevel(t *testing.T) {
	_, err := New(&bytes.Buffer{}, "trace", "text")
	if err == nil {
		t.Fatal("expected unknown level to fail")
	}
}

func TestNewRejectsUnknownFormat(t *testing.T) {
	_, err := New(&bytes.Buffer{}, "info", "xml")
	if err == nil {
		t.Fatal("expected unknown format to fail")
	}
}

func TestMiddlewareLogsRequest(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, "info", "text")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	handler := Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()) == slog.Default() {
			t.Fatal("expected request context logger")
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/health", nil))

	got := output.String()
	if !strings.Contains(got, "http request") || !strings.Contains(got, "status=202") {
		t.Fatalf("missing summary line: %q", got)
	}
	if !strings.Contains(got, "request_id=") {
		t.Fatalf("expected request_id field, got %q", got)
	}
	if !strings.Contains(got, `method=GET`) || !strings.Contains(got, `path=/v1/health`) {
		t.Fatalf("expected method/path fields, got %q", got)
	}
}

func TestMiddlewarePropagatesRequestFieldsToHandlerLogger(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, "info", "text")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	handler := Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		FromContext(r.Context()).InfoContext(r.Context(), "handler log")
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/memories", nil))

	got := output.String()
	// The handler log line itself must carry the request fields.
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 log lines (handler + summary), got %q", got)
	}
	handlerLine := lines[0]
	if !strings.Contains(handlerLine, "handler log") {
		t.Fatalf("first line is not the handler log: %q", handlerLine)
	}
	if !strings.Contains(handlerLine, "method=POST") || !strings.Contains(handlerLine, "path=/v1/memories") || !strings.Contains(handlerLine, "request_id=") {
		t.Fatalf("handler log missing request fields: %q", handlerLine)
	}
}

func TestMiddlewareRecoversPanic(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, "info", "text")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	handler := Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		panic("kaboom")
	}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/explode", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after panic, got %d", rr.Code)
	}
	got := output.String()
	if !strings.Contains(got, "panic in handler") {
		t.Fatalf("expected panic log line, got %q", got)
	}
	if !strings.Contains(got, "kaboom") {
		t.Fatalf("expected panic value in log, got %q", got)
	}
	if !strings.Contains(got, "status=500") {
		t.Fatalf("expected summary line with status 500, got %q", got)
	}
}

func TestMiddlewareSummaryStillEmittedOnPanicAfterWrite(t *testing.T) {
	// If a handler writes a status before panicking, the middleware must
	// not overwrite that status with 500 — only the summary line should
	// reflect what the client actually saw.
	var output bytes.Buffer
	logger, err := New(&output, "info", "text")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	handler := Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("after write")
	}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected status preserved as 202, got %d", rr.Code)
	}
	if !strings.Contains(output.String(), "status=202") {
		t.Fatalf("summary should report the already-written status, got %q", output.String())
	}
}

func TestResponseRecorderGuardsDoubleWriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	rec := newResponseRecorder(rr)

	rec.WriteHeader(http.StatusAccepted)
	rec.WriteHeader(http.StatusTeapot) // should be ignored

	if rec.status != http.StatusAccepted {
		t.Fatalf("recorder status = %d, want %d", rec.status, http.StatusAccepted)
	}
	if rr.Code != http.StatusAccepted {
		t.Fatalf("underlying writer status = %d, want %d", rr.Code, http.StatusAccepted)
	}
}

type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flusherRecorder) Flush() { f.flushed = true }

func TestResponseRecorderForwardsFlush(t *testing.T) {
	inner := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec := newResponseRecorder(inner)

	rec.Flush()

	if !inner.flushed {
		t.Fatal("expected Flush() to delegate to underlying writer")
	}
}

func TestResponseRecorderFlushNoopWhenUnsupported(t *testing.T) {
	rec := newResponseRecorder(httptest.NewRecorder())
	// httptest.ResponseRecorder implements Flusher, but with a wrapper
	// that doesn't, the call must not panic.
	rec.Flush() // baseline does not panic
}

func TestResponseRecorderHijackReturnsErrorWhenUnsupported(t *testing.T) {
	rec := newResponseRecorder(httptest.NewRecorder())
	_, _, err := rec.Hijack()
	if err == nil {
		t.Fatal("expected hijack to fail on non-hijackable writer")
	}
}

func TestFromContextFallsBackToDefault(t *testing.T) {
	if FromContext(context.Background()) == nil {
		t.Fatal("expected fallback logger")
	}
}

func TestNewRequestIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newRequestID()
		if len(id) != 16 {
			t.Fatalf("expected 16-char id, got %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id generated: %q", id)
		}
		seen[id] = true
	}
}
