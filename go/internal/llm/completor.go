package llm

import "context"

// Completor performs sentiment analysis and chat completion (live API or test stub).
type Completor interface {
	AnalyzeSignals(ctx context.Context, corpus string) (SentimentResult, error)
	Complete(ctx context.Context, system string, user string) (string, error)
}
