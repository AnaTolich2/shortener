package memory

import (
	"context"
	"sync"
	"time"

	"github.com/AnaTolich2/shortener/internal/entity"
)

type link struct {
	url       string
	expiresAt time.Time
}

type counter struct {
	n         int64
	expiresAt time.Time
}

type Store struct {
	mu       sync.Mutex
	now      func() time.Time
	links    map[string]link
	counters map[string]counter
}

func New(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{
		now:      now,
		links:    make(map[string]link),
		counters: make(map[string]counter),
	}
}

func expired(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && !now.Before(expiresAt)
}

func (s *Store) SaveNX(_ context.Context, code, rawURL string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if l, ok := s.links[code]; ok && !expired(l.expiresAt, now) {
		return false, nil
	}

	var exp time.Time
	if ttl > 0 {
		exp = now.Add(ttl)
	}
	s.links[code] = link{url: rawURL, expiresAt: exp}
	return true, nil
}

func (s *Store) Get(_ context.Context, code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.links[code]
	if !ok {
		return "", entity.ErrNotFound
	}
	if expired(l.expiresAt, s.now()) {
		delete(s.links, code)
		return "", entity.ErrNotFound
	}
	return l.url, nil
}

// Incr увеличивает счётчик. Первый вызов в окне создаёт счётчик со сроком жизни window.
// Возвращает новое значение и сколько осталось до конца окна.
func (s *Store) Incr(_ context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	c, ok := s.counters[key]
	if !ok || expired(c.expiresAt, now) {
		c = counter{expiresAt: now.Add(window)}
	}
	c.n++
	s.counters[key] = c

	return c.n, c.expiresAt.Sub(now), nil
}

func (s *Store) Ping(context.Context) error { return nil }
