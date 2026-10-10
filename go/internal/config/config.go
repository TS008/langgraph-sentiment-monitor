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
	// Self-hosted via docker-compose (see repo docker-compose.yml). Public busiyi.world is often Cloudflare-blocked.
	DefaultNewsNowBaseURL  = "http://127.0.0.1:4444"
	DefaultNewsNowInterval = 300 // seconds
)

// Config holds runtime settings loaded from the environment.
type Config struct {
	LLMAPIKey  string
	LLMBaseURL string
	LLMModel   string

	RSSFeeds []string

	NewsNowBaseURL     string
	NewsNowIDs         []string
	NewsNowMinInterval time.Duration

	HTTPAddr  string
	RedisURL  string
	QueueMode string // "memory" | "asynq"

	CrawlParallelism  int
	UseJSRender       bool
	LLMTimeout        time.Duration
	RunTimeout        time.Duration
	ApprovalTimeout   time.Duration
	MaxRetainedRuns   int
	MaxConcurrentRuns int
}

func Load() Config {
	_ = loadDotEnv()

	c := Config{
		LLMAPIKey:         llmAPIKeyFromEnv(),
		LLMBaseURL:        envOr("AEGIS_LLM_BASE_URL", DefaultLLMBaseURL),
		LLMModel:          envOr("AEGIS_LLM_MODEL", DefaultLLMModel),
		HTTPAddr:          envOr("AEGIS_HTTP_ADDR", ":8080"),
		RedisURL:          envOr("AEGIS_REDIS_URL", "redis://127.0.0.1:6379"),
		QueueMode:         strings.ToLower(envOr("AEGIS_QUEUE_MODE", "memory")),
		CrawlParallelism:  envInt("AEGIS_CRAWL_PARALLELISM", 4),
		UseJSRender:       envBool("AEGIS_CRAWL_JS_RENDER", false),
		LLMTimeout:        time.Duration(envInt("AEGIS_LLM_TIMEOUT_SEC", 120)) * time.Second,
		RunTimeout:        time.Duration(envInt("AEGIS_RUN_TIMEOUT_SEC", 900)) * time.Second,
		ApprovalTimeout:   time.Duration(envInt("AEGIS_APPROVAL_TIMEOUT_SEC", 5)) * time.Second,
		MaxRetainedRuns:   envInt("AEGIS_MAX_RETAINED_RUNS", 200),
		MaxConcurrentRuns: envInt("AEGIS_MAX_CONCURRENT_RUNS", 4),
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

	c.NewsNowBaseURL = envOr("AEGIS_NEWSNOW_BASE_URL", DefaultNewsNowBaseURL)
	c.NewsNowMinInterval = time.Duration(envInt("AEGIS_NEWSNOW_MIN_INTERVAL_SEC", DefaultNewsNowInterval)) * time.Second
	if !envBool("AEGIS_NEWSNOW_DISABLE", false) {
		c.NewsNowIDs = newsNowIDsFromEnv()
	}

	return c
}

func newsNowIDsFromEnv() []string {
	raw := os.Getenv("AEGIS_NEWSNOW_IDS")
	if raw == "" {
		return []string{"weibo", "zhihu", "baidu", "douyin", "toutiao", "thepaper"}
	}
	if strings.EqualFold(strings.TrimSpace(raw), "none") || raw == "-" {
		return nil
	}
	var ids []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			ids = append(ids, p)
		}
	}
	return ids
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
