package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AnaTolich2/shortener/internal/entity"
	"github.com/AnaTolich2/shortener/internal/httpapi"
	"github.com/AnaTolich2/shortener/internal/ratelimit"
	"github.com/AnaTolich2/shortener/internal/service"
	"github.com/AnaTolich2/shortener/internal/store/memory"
)

const baseURL = "http://short.test"


type fakeShortener struct {
	shorten func(ctx context.Context, rawURL string) (string, error)
	resolve func(ctx context.Context, code string) (string, error)
}

func (f fakeShortener) Shorten(ctx context.Context, rawURL string) (string, error) {
	return f.shorten(ctx, rawURL)
}

func (f fakeShortener) Resolve(ctx context.Context, code string) (string, error) {
	return f.resolve(ctx, code)
}

type fakeLimiter struct {
	allow  func(ctx context.Context, id string) (bool, time.Duration, error)
	lastID string
}

func (f *fakeLimiter) Allow(ctx context.Context, id string) (bool, time.Duration, error) {
	f.lastID = id
	return f.allow(ctx, id)
}

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func allowAll(context.Context, string) (bool, time.Duration, error) { return true, 0, nil }


func newRouter(svc httpapi.Shortener, lim httpapi.Limiter, pinger httpapi.Pinger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewRouter(httpapi.NewHandler(svc, baseURL, log), lim, pinger, log)
}

func do(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.7:51234"

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not valid JSON: %q (%v)", rec.Body.String(), err)
	}
	return v
}


func TestShorten_Created(t *testing.T) {
	svc := fakeShortener{shorten: func(_ context.Context, raw string) (string, error) {
		if raw != "https://example.com/page" {
			t.Errorf("service got url %q", raw)
		}
		return "abc1234", nil
	}}
	r := newRouter(svc, &fakeLimiter{allow: allowAll}, fakePinger{})

	rec := do(r, http.MethodPost, "/api/v1/shorten", `{"url":"https://example.com/page"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201, body: %s", rec.Code, rec.Body)
	}
	got := decode[map[string]string](t, rec)
	if got["code"] != "abc1234" || got["short_url"] != baseURL+"/r/abc1234" {
		t.Fatalf("unexpected body: %v", got)
	}
}

func TestShorten_BadRequest(t *testing.T) {
	called := false
	svc := fakeShortener{shorten: func(context.Context, string) (string, error) {
		called = true
		return "", nil
	}}
	r := newRouter(svc, &fakeLimiter{allow: allowAll}, fakePinger{})

	tests := map[string]string{
		"not json":        `url=https://example.com`,
		"empty body":      ``,
		"missing url":     `{}`,
		"empty url":       `{"url":""}`,
		"url is a number": `{"url":123}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			rec := do(r, http.MethodPost, "/api/v1/shorten", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d; want 400, body: %s", rec.Code, rec.Body)
			}
		})
	}
	if called {
		t.Fatal("the service must not be called for malformed requests")
	}
}

func TestShorten_InvalidURLFromService(t *testing.T) {
	svc := fakeShortener{shorten: func(context.Context, string) (string, error) {
		return "", fmt.Errorf("%w: scheme must be http or https", entity.ErrInvalidURL)
	}}
	r := newRouter(svc, &fakeLimiter{allow: allowAll}, fakePinger{})

	rec := do(r, http.MethodPost, "/api/v1/shorten", `{"url":"ftp://example.com"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", rec.Code)
	}
	if msg := decode[map[string]string](t, rec)["error"]; !strings.Contains(msg, "scheme must be http or https") {
		t.Fatalf("error message %q should explain what is wrong", msg)
	}
}

func TestShorten_InternalErrorHidesDetails(t *testing.T) {
	svc := fakeShortener{shorten: func(context.Context, string) (string, error) {
		return "", errors.New("dial tcp 10.0.0.5:6379: connection refused")
	}}
	r := newRouter(svc, &fakeLimiter{allow: allowAll}, fakePinger{})

	rec := do(r, http.MethodPost, "/api/v1/shorten", `{"url":"https://example.com"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Fatalf("internal details leaked to the client: %s", rec.Body)
	}
}


func TestRedirect(t *testing.T) {
	svc := fakeShortener{resolve: func(_ context.Context, code string) (string, error) {
		if code != "abc1234" {
			return "", entity.ErrNotFound
		}
		return "https://example.com/target?x=1", nil
	}}
	r := newRouter(svc, &fakeLimiter{allow: allowAll}, fakePinger{})

	rec := do(r, http.MethodGet, "/r/abc1234", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d; want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://example.com/target?x=1" {
		t.Fatalf("Location = %q", loc)
	}

	rec = do(r, http.MethodGet, "/r/unknown", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown code: status = %d; want 404", rec.Code)
	}
}

func TestRedirect_InternalError(t *testing.T) {
	svc := fakeShortener{resolve: func(context.Context, string) (string, error) {
		return "", errors.New("redis is down")
	}}
	r := newRouter(svc, &fakeLimiter{allow: allowAll}, fakePinger{})

	if rec := do(r, http.MethodGet, "/r/abc1234", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", rec.Code)
	}
}


func TestRateLimit_Denied(t *testing.T) {
	svcCalled := false
	svc := fakeShortener{shorten: func(context.Context, string) (string, error) {
		svcCalled = true
		return "abc1234", nil
	}}
	lim := &fakeLimiter{allow: func(context.Context, string) (bool, time.Duration, error) {
		return false, 42500 * time.Millisecond, nil
	}}
	r := newRouter(svc, lim, fakePinger{})

	rec := do(r, http.MethodPost, "/api/v1/shorten", `{"url":"https://example.com"}`)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d; want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "43" {
		t.Fatalf("Retry-After = %q; want 43 (42.5s rounded up)", got)
	}
	if svcCalled {
		t.Fatal("the service must not be called when the limit is exceeded")
	}
}

func TestRateLimit_UsesClientAddressNotForwardedHeader(t *testing.T) {
	svc := fakeShortener{shorten: func(context.Context, string) (string, error) { return "abc1234", nil }}
	lim := &fakeLimiter{allow: allowAll}
	r := newRouter(svc, lim, fakePinger{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/shorten", strings.NewReader(`{"url":"https://example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "1.1.1.1") // подделка: клиент пытается сменить «личность»
	req.RemoteAddr = "203.0.113.7:51234"
	r.ServeHTTP(httptest.NewRecorder(), req)

	if lim.lastID != "203.0.113.7" {
		t.Fatalf("limiter id = %q; want the real remote address 203.0.113.7", lim.lastID)
	}
}

func TestRateLimit_FailsOpenWhenLimiterBroken(t *testing.T) {
	svc := fakeShortener{shorten: func(context.Context, string) (string, error) { return "abc1234", nil }}
	lim := &fakeLimiter{allow: func(context.Context, string) (bool, time.Duration, error) {
		return false, 0, errors.New("redis is down")
	}}
	r := newRouter(svc, lim, fakePinger{})

	rec := do(r, http.MethodPost, "/api/v1/shorten", `{"url":"https://example.com"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201 (fail-open)", rec.Code)
	}
}


func TestHealth(t *testing.T) {
	svc := fakeShortener{}

	ok := newRouter(svc, &fakeLimiter{allow: allowAll}, fakePinger{})
	if rec := do(ok, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Fatalf("healthy: status = %d; want 200", rec.Code)
	}

	down := newRouter(svc, &fakeLimiter{allow: allowAll}, fakePinger{err: errors.New("no redis")})
	if rec := do(down, http.MethodGet, "/healthz", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("redis down: status = %d; want 503", rec.Code)
	}
}


func TestEndToEnd(t *testing.T) {
	store := memory.New(nil)
	svc := service.New(store, time.Hour)
	lim := ratelimit.New(store, 2, time.Minute)
	r := newRouter(svc, lim, store)

	rec := do(r, http.MethodPost, "/api/v1/shorten", `{"url":"https://example.com/e2e"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("shorten: status = %d, body: %s", rec.Code, rec.Body)
	}
	code := decode[map[string]string](t, rec)["code"]

	rec = do(r, http.MethodGet, "/r/"+code, "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://example.com/e2e" {
		t.Fatalf("redirect: status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = do(r, http.MethodPost, "/api/v1/shorten", `{"url":"javascript:alert(1)"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("javascript: url: status = %d; want 400", rec.Code)
	}

	rec = do(r, http.MethodPost, "/api/v1/shorten", `{"url":"https://example.com/third"}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("third request: status = %d, Retry-After = %q; want 429 with header",
			rec.Code, rec.Header().Get("Retry-After"))
	}

	if rec = do(r, http.MethodGet, "/r/"+code, ""); rec.Code != http.StatusFound {
		t.Fatalf("redirect after limit: status = %d; want 302", rec.Code)
	}
}
