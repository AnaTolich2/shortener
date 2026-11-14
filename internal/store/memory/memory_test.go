package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AnaTolich2/shortener/internal/entity"
	"github.com/AnaTolich2/shortener/internal/store/memory"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *clock { return &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)} }

func TestSaveNX(t *testing.T) {
	ctx := context.Background()
	s := memory.New(newClock().now)

	ok, err := s.SaveNX(ctx, "abc", "https://a.test", time.Hour)
	if err != nil || !ok {
		t.Fatalf("first SaveNX = %v, %v; want true, nil", ok, err)
	}

	ok, err = s.SaveNX(ctx, "abc", "https://b.test", time.Hour)
	if err != nil || ok {
		t.Fatalf("second SaveNX = %v, %v; want false, nil", ok, err)
	}

	got, err := s.Get(ctx, "abc")
	if err != nil || got != "https://a.test" {
		t.Fatalf("Get = %q, %v; want the first url", got, err)
	}
}

func TestGet_NotFoundAndExpired(t *testing.T) {
	ctx := context.Background()
	clk := newClock()
	s := memory.New(clk.now)

	if _, err := s.Get(ctx, "missing"); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("Get(missing) error = %v; want ErrNotFound", err)
	}

	if _, err := s.SaveNX(ctx, "abc", "https://a.test", time.Minute); err != nil {
		t.Fatal(err)
	}
	clk.advance(2 * time.Minute)

	if _, err := s.Get(ctx, "abc"); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("Get(expired) error = %v; want ErrNotFound", err)
	}

	ok, err := s.SaveNX(ctx, "abc", "https://c.test", time.Minute)
	if err != nil || !ok {
		t.Fatalf("SaveNX after expiry = %v, %v; want true, nil", ok, err)
	}
}

func TestIncr_WindowResets(t *testing.T) {
	ctx := context.Background()
	clk := newClock()
	s := memory.New(clk.now)

	for want := int64(1); want <= 3; want++ {
		n, ttl, err := s.Incr(ctx, "k", time.Minute)
		if err != nil || n != want {
			t.Fatalf("Incr = %d, %v; want %d", n, err, want)
		}
		if ttl <= 0 || ttl > time.Minute {
			t.Fatalf("ttl = %v; want in (0, 1m]", ttl)
		}
	}

	clk.advance(time.Minute)

	n, _, err := s.Incr(ctx, "k", time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("Incr after window = %d, %v; want 1", n, err)
	}
}

func TestSaveNX_Concurrent(t *testing.T) {
	ctx := context.Background()
	s := memory.New(nil)

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _ := s.SaveNX(ctx, "same", "https://x.test", time.Hour)
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d goroutines won SaveNX; want exactly 1", wins)
	}
}
