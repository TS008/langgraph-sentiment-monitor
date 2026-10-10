package store

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
)

const (
	redisRunKeyPrefix = "aegis:run:"
	redisRunIndexKey  = "aegis:runs"
	// DefaultRedisRunTTL is how long a run result survives in Redis after its last update.
	DefaultRedisRunTTL = 24 * time.Hour
	redisOpTimeout     = 3 * time.Second
)

// RedisRunStore keeps run results in Redis so the API process can serve runs that
// asynq workers execute. Each result is a JSON blob with a TTL; a sorted set
// (score = first-seen time) indexes the newest MaxRuns for listing.
type RedisRunStore struct {
	rdb    redis.UniversalClient
	max    int
	ttl    time.Duration
	logger *slog.Logger
}

func NewRedisRunStore(rdb redis.UniversalClient, max int, logger *slog.Logger) *RedisRunStore {
	if max < 1 {
		max = DefaultMaxRuns
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RedisRunStore{rdb: rdb, max: max, ttl: DefaultRedisRunTTL, logger: logger}
}

func (s *RedisRunStore) Put(r pipeline.RunResult) {
	data, err := json.Marshal(r)
	if err != nil {
		s.logger.Warn("run_store_put", "run_id", r.ID, "err", err)
		return
	}
	score := float64(time.Now().UnixNano())
	if !r.StartedAt.IsZero() {
		score = float64(r.StartedAt.UnixNano())
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	_, err = s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, redisRunKeyPrefix+r.ID, data, s.ttl)
		p.ZAddNX(ctx, redisRunIndexKey, redis.Z{Score: score, Member: r.ID})
		// Trimming only drops runs from the list view; their blobs expire via TTL.
		p.ZRemRangeByRank(ctx, redisRunIndexKey, 0, int64(-s.max-1))
		return nil
	})
	if err != nil {
		s.logger.Warn("run_store_put", "run_id", r.ID, "err", err)
	}
}

func (s *RedisRunStore) Get(id string) (pipeline.RunResult, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	data, err := s.rdb.Get(ctx, redisRunKeyPrefix+id).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			s.logger.Warn("run_store_get", "run_id", id, "err", err)
		}
		return pipeline.RunResult{}, false
	}
	var r pipeline.RunResult
	if err := json.Unmarshal(data, &r); err != nil {
		s.logger.Warn("run_store_get", "run_id", id, "err", err)
		return pipeline.RunResult{}, false
	}
	return r, true
}

func (s *RedisRunStore) List(limit int) []RunSummary {
	if limit <= 0 || limit > s.max {
		limit = s.max
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	ids, err := s.rdb.ZRevRange(ctx, redisRunIndexKey, 0, int64(limit-1)).Result()
	if err != nil || len(ids) == 0 {
		if err != nil {
			s.logger.Warn("run_store_list", "err", err)
		}
		return []RunSummary{}
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = redisRunKeyPrefix + id
	}
	vals, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		s.logger.Warn("run_store_list", "err", err)
		return []RunSummary{}
	}
	out := make([]RunSummary, 0, len(vals))
	var expired []interface{}
	for i, v := range vals {
		str, ok := v.(string)
		if !ok {
			expired = append(expired, ids[i])
			continue
		}
		var r pipeline.RunResult
		if json.Unmarshal([]byte(str), &r) == nil {
			out = append(out, summarize(r))
		}
	}
	if len(expired) > 0 {
		s.rdb.ZRem(ctx, redisRunIndexKey, expired...)
	}
	return out
}

func (s *RedisRunStore) Len() int {
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	n, err := s.rdb.ZCard(ctx, redisRunIndexKey).Result()
	if err != nil {
		return 0
	}
	return int(n)
}
