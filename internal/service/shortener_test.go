package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AnaTolich2/shortener/internal/entity"
	"github.com/AnaTolich2/shortener/internal/store/memory"
)

func sequence(codes ...string) func(int) (string, error) {
	i := 0
	return func(int) (string, error) {
		code := codes[i%len(codes)]
		i++
		return code, nil
	}
}

type failingStore struct{ err error }

func (f failingStore) SaveNX(context.Context, string, string, time.Duration) (bool, error) {
	return false, f.err
}
func (f failingStore) Get(context.Context, string) (string, error) { return "", f.err }

type countingStore struct{ gets int }

func (c *countingStore) SaveNX(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (c *countingStore) Get(context.Context, string) (string, error) {
	c.gets++
	return "", entity.ErrNotFound
}

func TestShortenAndResolve(t *testing.T) {
	ctx := context.Background()
	svc := New(memory.New(nil), time.Hour)

	code, err := svc.Shorten(ctx, "https://example.com/some/path?q=1")
	if err != nil {
		t.Fatalf("Shorten() error = %v", err)
	}
	if len(code) != defaultCodeLen {
		t.Errorf("code %q has length %d; want %d", code, len(code), defaultCodeLen)
	}

	got, err := svc.Resolve(ctx, code)
	if err != nil || got != "https://example.com/some/path?q=1" {
		t.Fatalf("Resolve() = %q, %v", got, err)
	}
}

func TestShorten_InvalidURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"not a url", "not a url"},
		{"ftp scheme", "ftp://example.com/file"},
		{"javascript scheme", "javascript:alert(1)"},
		{"no host", "http://"},
		{"too long", "https://example.com/" + strings.Repeat("a", maxURLLength)},
	}

	svc := New(memory.New(nil), time.Hour)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Shorten(context.Background(), tt.url)
			if !errors.Is(err, entity.ErrInvalidURL) {
				t.Fatalf("Shorten(%q) error = %v; want ErrInvalidURL", tt.url, err)
			}
		})
	}
}

func TestShorten_RetriesOnCollision(t *testing.T) {
	ctx := context.Background()
	svc := New(memory.New(nil), time.Hour, WithCodeGenerator(sequence("aaaaaaa", "aaaaaaa", "bbbbbbb")))

	first, err := svc.Shorten(ctx, "https://one.test")
	if err != nil || first != "aaaaaaa" {
		t.Fatalf("first Shorten() = %q, %v; want aaaaaaa", first, err)
	}

	second, err := svc.Shorten(ctx, "https://two.test")
	if err != nil || second != "bbbbbbb" {
		t.Fatalf("second Shorten() = %q, %v; want bbbbbbb after two collisions", second, err)
	}

	got, err := svc.Resolve(ctx, "aaaaaaa")
	if err != nil || got != "https://one.test" {
		t.Fatalf("Resolve(aaaaaaa) = %q, %v; want the first url", got, err)
	}
}

func TestShorten_GivesUpAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	st := memory.New(nil)
	if _, err := st.SaveNX(ctx, "taken00", "https://old.test", time.Hour); err != nil {
		t.Fatal(err)
	}

	svc := New(st, time.Hour, WithMaxAttempts(3), WithCodeGenerator(sequence("taken00")))

	_, err := svc.Shorten(ctx, "https://new.test")
	if !errors.Is(err, entity.ErrCodeCollision) {
		t.Fatalf("Shorten() error = %v; want ErrCodeCollision", err)
	}
}

func TestShorten_StoreError(t *testing.T) {
	boom := errors.New("redis is down")
	svc := New(failingStore{err: boom}, time.Hour)

	_, err := svc.Shorten(context.Background(), "https://example.com")
	if !errors.Is(err, boom) {
		t.Fatalf("Shorten() error = %v; want it to wrap the store error", err)
	}
}

func TestResolve_NotFound(t *testing.T) {
	svc := New(memory.New(nil), time.Hour)

	_, err := svc.Resolve(context.Background(), "nope123")
	if !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("Resolve() error = %v; want ErrNotFound", err)
	}
}

func TestResolve_StoreErrorIsNotNotFound(t *testing.T) {
	boom := errors.New("redis is down")
	svc := New(failingStore{err: boom}, time.Hour)

	_, err := svc.Resolve(context.Background(), "abc1234")
	if errors.Is(err, entity.ErrNotFound) || !errors.Is(err, boom) {
		t.Fatalf("Resolve() error = %v; want the store error, not ErrNotFound", err)
	}
}

func TestResolve_GarbageCodeSkipsStore(t *testing.T) {
	st := &countingStore{}
	svc := New(st, time.Hour)

	for _, code := range []string{"", "bad code", "x/../y", "code-with-dash", strings.Repeat("a", maxCodeLength+1)} {
		if _, err := svc.Resolve(context.Background(), code); !errors.Is(err, entity.ErrNotFound) {
			t.Errorf("Resolve(%q) error = %v; want ErrNotFound", code, err)
		}
	}
	if st.gets != 0 {
		t.Fatalf("store was called %d times for invalid codes; want 0", st.gets)
	}
}

func TestResolve_Expired(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := New(memory.New(func() time.Time { return now }), time.Hour)

	code, err := svc.Shorten(ctx, "https://example.com")
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Hour)

	if _, err := svc.Resolve(ctx, code); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("Resolve() after TTL error = %v; want ErrNotFound", err)
	}
}

func TestRandomCode(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		code, err := randomCode(defaultCodeLen)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != defaultCodeLen || !validCode(code) {
			t.Fatalf("randomCode() = %q; want %d base62 characters", code, defaultCodeLen)
		}
		seen[code] = struct{}{}
	}
	if len(seen) < 990 {
		t.Fatalf("only %d unique codes out of 1000", len(seen))
	}
}
