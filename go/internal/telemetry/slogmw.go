package telemetry

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// SlogMiddleware logs requests with request id (structured).
func SlogMiddleware(logger *slog.Logger, m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			if m != nil {
				m.HTTPRequests.Add(1)
			}
			// Container readiness probes hit health endpoints every few seconds.
			level := slog.LevelInfo
			if r.URL.Path == "/healthz" || r.URL.Path == "/health" {
				level = slog.LevelDebug
			}
			logger.Log(r.Context(), level, "http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
