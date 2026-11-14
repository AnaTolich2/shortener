package ratelimit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AnaTolich2/shortener/internal/ratelimit"
	"github.com/AnaTolich2/shortener/internal/store/memory"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

type failingCounter struct{ err error }

func (f failingCounter) Incr(context.Context, string, time.Duration) (int64, time.Duration, error) {
	return 0, 0, f.err
}

func newClock() *clock { return &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)} }

func TestLimiter_BlocksAfterLimit(t *testing.T) {
	ctx := context.Background()
	l := ratelimit.New(memory.New(newClock().now), 3, time.Minute)

	for i := 1; i <= 3; i++ {
		allowed, _, err := l.Allow(ctx, "1.2.3.4")
		if err != nil || !allowed {
			t.Fatalf("request %d: allowed=%v err=%v; want allowed", i, allowed, err)
		}
	}

	allowed, retryAfter, err := l.Allow(ctx, "1.2.3.4")
	if err != nil || allowed {
		t.Fatalf("4th request: allowed=%v err=%v; want denied", allowed, err)
	}
	if retryAfter <= 0 || retryAfter > time.Minute {
		t.Fatalf("retryAfter = %v; want in (0, 1m]", retryAfter)
	}
}

func TestLimiter_WindowReset(t *testing.T) {
	ctx := context.Background()
	clk := newClock()
	l := ratelimit.New(memory.New(clk.now), 1, time.Minute)

	if allowed, _, _ := l.Allow(ctx, "ip"); !allowed {
		t.Fatal("first request must be allowed")
	}
	if allowed, _, _ := l.Allow(ctx, "ip"); allowed {
		t.Fatal("second request in the same window must be denied")
	}

	clk.t = clk.t.Add(time.Minute)

	if allowed, _, _ := l.Allow(ctx, "ip"); !allowed {
		t.Fatal("request in the next window must be allowed")
	}
}

func TestLimiter_RetryAfterShrinks(t *testing.T) {
	ctx := context.Background()
	clk := newClock()
	l := ratelimit.New(memory.New(clk.now), 1, time.Minute)

	l.Allow(ctx, "ip")
	_, first, _ := l.Allow(ctx, "ip")

	clk.t = clk.t.Add(20 * time.Second)
	_, second, _ := l.Allow(ctx, "ip")

	if second != first-20*time.Second {
		t.Fatalf("retryAfter went from %v to %v; want it to shrink by 20s", first, second)
	}
}

func TestLimiter_IndependentClients(t *testing.T) {
	ctx := context.Background()
	l := ratelimit.New(memory.New(newClock().now), 1, time.Minute)

	if allowed, _, _ := l.Allow(ctx, "a"); !allowed {
		t.Fatal("client a must be allowed")
	}
	if allowed, _, _ := l.Allow(ctx, "b"); !allowed {
		t.Fatal("client b has its own counter and must be allowed")
	}
}

func TestLimiter_CounterError(t *testing.T) {
	boom := errors.New("redis is down")
	l := ratelimit.New(failingCounter{err: boom}, 1, time.Minute)

	allowed, _, err := l.Allow(context.Background(), "ip")
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v; want it to wrap the counter error", err)
	}
	if allowed {
		t.Fatal("on error the limiter must not claim the request is allowed; the caller decides")
	}
}
