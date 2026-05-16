// Package logging owns the standard-library log/slog setup, the per-request
// HTTP middleware, and the context-based logger plumbing used across Mnemo.
//
// The package is intentionally small: New constructs a logger from CLI flags,
// WithLogger/FromContext propagate it through context.Context, and Middleware
// wraps an http.Handler with request-scoped enrichment (method, path,
// request_id), structured panic recovery, and a single summary line per
// request.
package logging

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"
)

const (
	// DefaultLevel is "warn" deliberately: Mnemo is a developer tool and
	// most users won't want a chatty terminal during CLI work. Surfaces
	// that benefit from more verbosity (the API server, the UI server)
	// can be invoked with --log-level=info.
	DefaultLevel  = "warn"
	DefaultFormat = "text"
)

type contextKey struct{}

// New constructs a slog.Logger that writes to writer at the given level and
// format. Format must be "text" (default) or "json"; level must be one of
// "debug", "info", "warn", or "error".
func New(writer io.Writer, level string, format string) (*slog.Logger, error) {
	parsedLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	opts := &slog.HandlerOptions{Level: parsedLevel}
	switch format {
	case "", "text":
		return slog.New(slog.NewTextHandler(writer, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(writer, opts)), nil
	default:
		return nil, fmt.Errorf("unsupported log format %q", format)
	}
}

// WithLogger returns a child context carrying logger. A nil logger is a
// no-op so callers don't have to guard.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext returns the logger carried by ctx, or slog.Default() when none
// is set. Never returns nil.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}

// Middleware wraps next with structured request logging. It:
//   - generates a request_id and attaches method, path, request_id to a
//     per-request logger
//   - stuffs the enriched logger into the request context so handlers can
//     reach it via FromContext and inherit the same fields
//   - recovers panics, logs them at error level with stack trace, and returns
//     500 to the client
//   - emits a single "http request" summary line at info level with status
//     and duration on every request, success or panic
func Middleware(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := newResponseRecorder(w)

		reqLogger := logger.With(
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("request_id", newRequestID()),
		)
		ctx := WithLogger(r.Context(), reqLogger)

		defer func() {
			if rec := recover(); rec != nil {
				reqLogger.ErrorContext(ctx, "panic in handler",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				if !recorder.wroteHeader {
					http.Error(recorder, "internal server error", http.StatusInternalServerError)
				}
			}
			reqLogger.InfoContext(ctx, "http request",
				slog.Int("status", recorder.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("remote_addr", r.RemoteAddr),
			)
		}()

		next.ServeHTTP(recorder, r.WithContext(ctx))
	})
}

// responseRecorder captures the status code of an http.ResponseWriter and
// forwards optional capabilities (http.Flusher, http.Hijacker) when the
// underlying writer supports them. Double WriteHeader calls are silently
// dropped after the first.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// Flush delegates to the underlying writer when it supports http.Flusher;
// otherwise it is a no-op so streaming handlers degrade gracefully.
func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack delegates to the underlying writer when it supports http.Hijacker;
// otherwise it returns an error so callers can detect that connection
// hijacking is unavailable through this middleware.
func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("logging.responseRecorder: underlying ResponseWriter does not support Hijacker")
}

// newRequestID returns a 16-character hex string sourced from crypto/rand.
// It falls back to a time-based suffix only if the system RNG is unavailable.
func newRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("t%d", time.Now().UnixNano())
}

func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "", "warn":
		return slog.LevelWarn, nil
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", level)
	}
}
