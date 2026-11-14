// Package redisstore — хранилище на Redis: короткие ссылки и счётчики для rate limit.
package redisstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AnaTolich2/shortener/internal/entity"
)

const linkPrefix = "url:"

var incrScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('PTTL', KEYS[1])
return {count, ttl}
`)

type Store struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

func (s *Store) SaveNX(ctx context.Context, code, rawURL string, ttl time.Duration) (bool, error) {
	return s.rdb.SetNX(ctx, linkPrefix+code, rawURL, ttl).Result()
}

func (s *Store) Get(ctx context.Context, code string) (string, error) {
	val, err := s.rdb.Get(ctx, linkPrefix+code).Result()
	if errors.Is(err, redis.Nil) {
		return "", entity.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (s *Store) Incr(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	res, err := incrScript.Run(ctx, s.rdb, []string{key}, window.Milliseconds()).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	if len(res) != 2 {
		return 0, 0, fmt.Errorf("unexpected script result: %v", res)
	}

	ttl := time.Duration(res[1]) * time.Millisecond
	if ttl <= 0 {
		ttl = window
	}
	return res[0], ttl, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.rdb.Ping(ctx).Err()
}
