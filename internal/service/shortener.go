package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/AnaTolich2/shortener/internal/entity"
)

const (
	alphabet    = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	acceptLimit = byte(256 - 256%len(alphabet))

	defaultCodeLen     = 7
	defaultMaxAttempts = 5
	maxURLLength       = 2048
	maxCodeLength      = 32
)

type urlStore interface {
	SaveNX(ctx context.Context, code, rawURL string, ttl time.Duration) (bool, error)
	Get(ctx context.Context, code string) (string, error)
}

type Shortener struct {
	store       urlStore
	ttl         time.Duration
	codeLen     int
	maxAttempts int
	newCode     func(n int) (string, error)
}

type Option func(*Shortener)

func WithCodeLength(n int) Option {
	return func(s *Shortener) {
		if n > 0 {
			s.codeLen = n
		}
	}
}

func WithMaxAttempts(n int) Option {
	return func(s *Shortener) {
		if n > 0 {
			s.maxAttempts = n
		}
	}
}

func WithCodeGenerator(g func(n int) (string, error)) Option {
	return func(s *Shortener) {
		if g != nil {
			s.newCode = g
		}
	}
}

func New(store urlStore, ttl time.Duration, opts ...Option) *Shortener {
	s := &Shortener{
		store:       store,
		ttl:         ttl,
		codeLen:     defaultCodeLen,
		maxAttempts: defaultMaxAttempts,
		newCode:     randomCode,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Shortener) Shorten(ctx context.Context, rawURL string) (string, error) {
	if err := validateURL(rawURL); err != nil {
		return "", err
	}

	for attempt := 0; attempt < s.maxAttempts; attempt++ {
		code, err := s.newCode(s.codeLen)
		if err != nil {
			return "", fmt.Errorf("generate code: %w", err)
		}

		saved, err := s.store.SaveNX(ctx, code, rawURL, s.ttl)
		if err != nil {
			return "", fmt.Errorf("save link: %w", err)
		}
		if saved {
			return code, nil
		}
	}

	return "", entity.ErrCodeCollision
}

func (s *Shortener) Resolve(ctx context.Context, code string) (string, error) {
	if !validCode(code) {
		return "", entity.ErrNotFound
	}

	target, err := s.store.Get(ctx, code)
	if err != nil {
		if errors.Is(err, entity.ErrNotFound) {
			return "", err
		}
		return "", fmt.Errorf("get link: %w", err)
	}
	return target, nil
}

func validateURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("%w: empty", entity.ErrInvalidURL)
	}
	if len(raw) > maxURLLength {
		return fmt.Errorf("%w: longer than %d characters", entity.ErrInvalidURL, maxURLLength)
	}

	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return fmt.Errorf("%w: cannot parse", entity.ErrInvalidURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme must be http or https", entity.ErrInvalidURL)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: host is empty", entity.ErrInvalidURL)
	}
	return nil
}

func validCode(code string) bool {
	if code == "" || len(code) > maxCodeLength {
		return false
	}
	for i := 0; i < len(code); i++ {
		if !isBase62(code[i]) {
			return false
		}
	}
	return true
}

func isBase62(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func randomCode(n int) (string, error) {
	out := make([]byte, 0, n)
	buf := make([]byte, n)

	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= acceptLimit {
				continue
			}
			out = append(out, alphabet[int(b)%len(alphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}
