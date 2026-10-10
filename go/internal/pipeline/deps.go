package pipeline

import (
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/llm"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/social"
)

// OrchestratorDeps injects collaborators (nil fields use production defaults).
type OrchestratorDeps struct {
	Completor llm.Completor
	Social    *social.Registry
}
