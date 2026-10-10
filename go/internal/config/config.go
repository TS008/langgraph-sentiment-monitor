package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	DefaultLLMBaseURL = "https://opencode.ai/zen/go/v1"
	DefaultLLMModel   = "mimo-v2.6-flash"
)

// Config holds runtime settings loaded from the environment.
type Config struct {
	LLMAPIKey  string
	LLMBaseURL string
	LLMModel   string

	RSSFeeds []string

	HTTPAddr string
	RedisURL string
	QueueMode string // "memory" | "asynq"

	CrawlParallelism int
	UseJSRender      bool
	LLMTimeout       time.Duration
	ApprovalTimeout  time.Duration
}

func Load() Config {
	_ = loadDotEnv()

	c := Config{
		LLMAPIKey:  llmAPIKeyFromEnv(),
		LLMBaseURL: envOr("AEGIS_LLM_BASE_URL", DefaultLLMBaseURL),
		LLMModel:   envOr("AEGIS_LLM_MODEL", DefaultLLMModel),
		HTTPAddr:   envOr("AEGIS_HTTP_ADDR", ":8080"),
		RedisURL:   envOr("AEGIS_REDIS_URL", "redis://127.0.0.1:6379"),
		QueueMode:  strings.ToLower(envOr("AEGIS_QUEUE_MODE", "memory")),
		CrawlParallelism: envInt("AEGIS_CRAWL_PARALLELISM", 4),
		UseJSRender:      envBool("AEGIS_CRAWL_JS_RENDER", false),
		LLMTimeout:       time.Duration(envInt("AEGIS_LLM_TIMEOUT_SEC", 60)) * time.Second,
		ApprovalTimeout:  time.Duration(envInt("AEGIS_APPROVAL_TIMEOUT_SEC", 5)) * time.Second,
	}

	if feeds := os.Getenv("AEGIS_RSS_FEEDS"); feeds != "" {
		for _, f := range strings.Split(feeds, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				c.RSSFeeds = append(c.RSSFeeds, f)
			}
		}
	}
	if len(c.RSSFeeds) == 0 {
		c.RSSFeeds = []string{"https://hnrss.org/frontpage"}
	}

	return c
}

func llmAPIKeyFromEnv() string {
	if k := os.Getenv("OPENCODE_GO_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("AEGIS_LLM_API_KEY")
}

func (c Config) LLMEnabled() bool {
	k := c.LLMAPIKey
	return k != "" && k != "your_api_key_here" && !strings.HasPrefix(k, "your_")
}

func (c Config) UseAsynq() bool {
	return c.QueueMode == "asynq"
}

func loadDotEnv() error {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		if _, err := os.Stat(path); err == nil {
			return godotenv.Load(path)
		}
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
