package llm

import "context"

// StubCompletor is an offline LLM for unit tests (no network, no secrets).
type StubCompletor struct {
	Sentiment SentimentResult
	Reply     string
}

func NewStubCompletor() *StubCompletor {
	return &StubCompletor{
		Sentiment: SentimentResult{
			RiskScore:   42,
			Sentiment:   "mixed",
			Summary:     "stub sentiment",
			KeyTopics:   []string{"test"},
			Recommended: "monitor",
		},
		Reply: "stub llm response",
	}
}

func (s *StubCompletor) AnalyzeSignals(_ context.Context, _ string) (SentimentResult, error) {
	return s.Sentiment, nil
}

func (s *StubCompletor) Complete(_ context.Context, _ string, _ string) (string, error) {
	return s.Reply, nil
}
