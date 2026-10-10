package llm

import "context"

const (
	opencodeSessionHeader = "x-opencode-session"
	userAgentValue        = "aegis-monitor/1.0"
)

type sessionKey struct{}

// WithSession attaches a stable OpenCode session id (reuse for all LLM calls in one pipeline run).
func WithSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionKey{}, sessionID)
}

// SessionFromContext returns the OpenCode session id for the request, if any.
func SessionFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(sessionKey{}).(string)
	return v
}
