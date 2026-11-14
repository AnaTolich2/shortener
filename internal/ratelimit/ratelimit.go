package ratelimit

import (
	"context"
	"fmt"
	"time"
)

type Counter interface {
	Incr(ctx context.Context, key string, window time.Duration) (count int64, ttl time.Duration, err error)
}

type Limiter struct {
	counter Counter
	limit   int64
	window  time.Duration
}

func New(counter Counter, limit int64, window time.Duration) *Limiter {
	return &Limiter{counter: counter, limit: limit, window: window}
}

func (l *Limiter) Allow(ctx context.Context, id string) (bool, time.Duration, error) {
	count, ttl, err := l.counter.Incr(ctx, "rl:"+id, l.window)
	if err != nil {
		return false, 0, fmt.Errorf("rate limiter counter: %w", err)
	}
	if count > l.limit {
		return false, ttl, nil
	}
	return true, 0, nil
}
