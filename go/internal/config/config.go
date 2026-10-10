package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds runtime settings loaded from the environment.
type Config struct {
	DeepSeekAPIKey string
	DeepSeekBase   string
	DeepSeekModel  string

	RSSFeeds []string

	HTTPAddr string

	CrawlParallelism int
	LLMTimeout       time.Duration
	ApprovalTimeout  time.Duration
}

func Load() Config {
	_ = loadDotEnv()

	c := Config{
		DeepSeekAPIKey: os.Getenv("DEEPSEEK_API_KEY"),
		DeepSeekBase:   envOr("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		DeepSeekModel:  envOr("DEEPSEEK_MODEL", "deepseek-chat"),
		HTTPAddr:       envOr("AEGIS_HTTP_ADDR", ":8080"),
		CrawlParallelism: envInt("AEGIS_CRAWL_PARALLELISM", 4),
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

func (c Config) LLMEnabled() bool {
	return c.DeepSeekAPIKey != "" && c.DeepSeekAPIKey != "your_api_key_here"
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
