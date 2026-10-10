package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/pipeline"
)

func newRedisStore(t *testing.T, max int) (*RedisRunStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return NewRedisRunStore(rdb, max, nil), mr
}

func TestRedisStoreSharedAcrossInstances(t *testing.T) {
	writer, mr := newRedisStore(t, 10)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	reader := NewRedisRunStore(rdb, 10, nil)

	writer.Put(pipeline.RunResult{ID: "r1", Status: "running", StartedAt: time.Now()})
	writer.Put(pipeline.RunResult{ID: "r1", Status: "completed", StartedAt: time.Now(), Diagnosis: "d"})

	got, ok := reader.Get("r1")
	if !ok || got.Status != "completed" || got.Diagnosis != "d" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	if reader.Len() != 1 {
		t.Fatalf("len=%d; updates must not duplicate index entries", reader.Len())
	}
	if ttl := mr.TTL(redisRunKeyPrefix + "r1"); ttl <= 0 || ttl > DefaultRedisRunTTL {
		t.Fatalf("ttl=%v", ttl)
	}
	if _, ok := reader.Get("missing"); ok {
		t.Fatal("missing run should not be found")
	}
}

func TestRedisStoreListNewestFirstAndTrims(t *testing.T) {
	s, _ := newRedisStore(t, 3)
	base := time.Now()
	for i := 0; i < 5; i++ {
		s.Put(pipeline.RunResult{ID: fmt.Sprint(i), Status: "completed", StartedAt: base.Add(time.Duration(i) * time.Second)})
	}
	if s.Len() != 3 {
		t.Fatalf("index len=%d want 3", s.Len())
	}
	got := s.List(2)
	if len(got) != 2 || got[0].ID != "4" || got[1].ID != "3" {
		t.Fatalf("list=%+v", got)
	}
}

func TestRedisStoreListDropsExpiredEntries(t *testing.T) {
	s, mr := newRedisStore(t, 10)
	s.Put(pipeline.RunResult{ID: "old", Status: "completed", StartedAt: time.Now().Add(-time.Hour)})
	s.Put(pipeline.RunResult{ID: "new", Status: "completed", StartedAt: time.Now()})
	mr.Del(redisRunKeyPrefix + "old")

	got := s.List(10)
	if len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("list=%+v", got)
	}
	if s.Len() != 1 {
		t.Fatalf("expired id should be pruned from index, len=%d", s.Len())
	}
}

var _ RunStore = (*RedisRunStore)(nil)
var _ RunStore = (*MemoryRunStore)(nil)
