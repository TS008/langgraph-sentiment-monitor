package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/textutil"

	openai "github.com/sashabaranov/go-openai"
)

// Client wraps OpenAI-compatible chat APIs (OpenCode Go, etc.).
type Client struct {
	cfg    config.Config
	client *openai.Client
}

func New(cfg config.Config) *Client {
	c := &Client{cfg: cfg}
	if cfg.LLMEnabled() {
		ocfg := openai.DefaultConfig(cfg.LLMAPIKey)
		base := strings.TrimRight(cfg.LLMBaseURL, "/")
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		ocfg.BaseURL = base
		ocfg.HTTPClient = newOpenCodeHTTPClient()
		c.client = openai.NewClientWithConfig(ocfg)
	}
	return c
}

// SentimentResult is structured LLM output for crisis monitoring.
type SentimentResult struct {
	RiskScore   int      `json:"risk_score"` // 0-100
	Sentiment   string   `json:"sentiment"`  // negative|neutral|positive|mixed
	Summary     string   `json:"summary"`
	KeyTopics   []string `json:"key_topics"`
	Recommended string   `json:"recommended_action"`
}

func (c *Client) AnalyzeSignals(ctx context.Context, corpus string) (SentimentResult, error) {
	if !c.cfg.LLMEnabled() {
		return heuristicAnalyze(corpus), nil
	}

	prompt := `You are an enterprise crisis sentiment analyst. Given public web/social signal text, respond with ONLY valid JSON (no markdown) matching:
{"risk_score":0-100,"sentiment":"negative|neutral|positive|mixed","summary":"...","key_topics":["..."],"recommended_action":"..."}

Signals:
` + corpus

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.cfg.LLMModel,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   600,
	})
	if err != nil {
		if isAuthError(err) {
			return heuristicAnalyze(corpus), nil
		}
		return SentimentResult{}, err
	}
	if len(resp.Choices) == 0 {
		return SentimentResult{}, fmt.Errorf("empty LLM response")
	}
	raw := strings.TrimSpace(resp.Choices[0].Message.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	var out SentimentResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return SentimentResult{
			RiskScore:   50,
			Sentiment:   "mixed",
			Summary:     raw,
			KeyTopics:   []string{"parse-fallback"},
			Recommended: "Review LLM output manually",
		}, nil
	}
	return out, nil
}

func (c *Client) Complete(ctx context.Context, system string, user string) (string, error) {
	if !c.cfg.LLMEnabled() {
		return "[offline] " + truncate(user, 200), nil
	}
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.cfg.LLMModel,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: system},
			{Role: openai.ChatMessageRoleUser, Content: user},
		},
		Temperature: 0.5,
		MaxTokens:   800,
	})
	if err != nil {
		if isAuthError(err) {
			return "[offline-fallback] " + truncate(user, 300), nil
		}
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty LLM response")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

// isAuthError reports credential failures, which degrade to offline output
// (transient 429/5xx are retried in the transport instead).
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "invalid api key") ||
		strings.Contains(msg, "authentication")
}

func heuristicAnalyze(corpus string) SentimentResult {
	lower := strings.ToLower(corpus)
	score := 25
	sentiment := "neutral"
	neg := []string{"outage", "down", "fail", "attack", "lawsuit", "breach", "angry", "宕机", "失败", "危机", "负面"}
	for _, w := range neg {
		if strings.Contains(lower, w) {
			score += 12
		}
	}
	if score > 85 {
		score = 85
	}
	if score >= 55 {
		sentiment = "negative"
	}
	return SentimentResult{
		RiskScore:   score,
		Sentiment:   sentiment,
		Summary:     "Heuristic analysis (no API key): " + truncate(corpus, 120),
		KeyTopics:   []string{"heuristic"},
		Recommended: "Set OPENCODE_GO_API_KEY for full LLM scoring",
	}
}

func truncate(s string, n int) string {
	return textutil.Truncate(s, n)
}
