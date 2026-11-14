package redisstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AnaTolich2/shortener/internal/entity"
	"github.com/AnaTolich2/shortener/internal/store/redisstore"
)

func newStore(t *testing.T) *redisstore.Store {
	t.Helper()

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { rdb.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis is not available at %s: %v", addr, err)
	}
	return redisstore.New(rdb)
}

func uniq(name string) string {
	return fmt.Sprintf("test-%s-%d", name, time.Now().UnixNano())
}

func TestSaveNXAndGet(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	code := uniq("save")

	ok, err := s.SaveNX(ctx, code, "https://a.test", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first SaveNX = %v, %v; want true, nil", ok, err)
	}

	ok, err = s.SaveNX(ctx, code, "https://b.test", time.Minute)
	if err != nil || ok {
		t.Fatalf("second SaveNX = %v, %v; want false, nil", ok, err)
	}

	got, err := s.Get(ctx, code)
	if err != nil || got != "https://a.test" {
		t.Fatalf("Get = %q, %v; want the first url", got, err)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newStore(t)

	_, err := s.Get(context.Background(), uniq("missing"))
	if !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("Get error = %v; want ErrNotFound", err)
	}
}

func TestSaveNX_Expires(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	code := uniq("ttl")

	if _, err := s.SaveNX(ctx, code, "https://a.test", 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)

	if _, err := s.Get(ctx, code); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("Get after TTL error = %v; want ErrNotFound", err)
	}
}

func TestSaveNX_ConcurrentExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	code := uniq("race")

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.SaveNX(ctx, code, "https://x.test", time.Minute)
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d goroutines won; want exactly 1", wins)
	}
}

func TestIncr_CountsAndSetsTTL(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	key := uniq("incr")

	for want := int64(1); want <= 3; want++ {
		n, ttl, err := s.Incr(ctx, key, time.Minute)
		if err != nil || n != want {
			t.Fatalf("Incr = %d, %v; want %d", n, err, want)
		}
		if ttl <= 0 || ttl > time.Minute {
			t.Fatalf("ttl = %v; want in (0, 1m]", ttl)
		}
	}
}

func TestIncr_WindowExpires(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	key := uniq("window")

	if _, _, err := s.Incr(ctx, key, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)

	n, _, err := s.Incr(ctx, key, 200*time.Millisecond)
	if err != nil || n != 1 {
		t.Fatalf("Incr after window = %d, %v; want 1", n, err)
	}
}

func TestIncr_ConcurrentIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	key := uniq("atomic")

	const workers = 50
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.Incr(ctx, key, time.Minute); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	n, _, err := s.Incr(ctx, key, time.Minute)
	if err != nil || n != workers+1 {
		t.Fatalf("counter = %d, %v; want %d (no lost updates)", n, err, workers+1)
	}
}

func TestPing(t *testing.T) {
	if err := newStore(t).Ping(context.Background()); err != nil {
		t.Fatalf("Ping error = %v", err)
	}
}
