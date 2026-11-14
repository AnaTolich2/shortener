package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AnaTolich2/shortener/internal/config"
	"github.com/AnaTolich2/shortener/internal/httpapi"
	"github.com/AnaTolich2/shortener/internal/ratelimit"
	"github.com/AnaTolich2/shortener/internal/service"
	"github.com/AnaTolich2/shortener/internal/store/redisstore"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		return fmt.Errorf("connect to redis at %s: %w", cfg.RedisAddr, err)
	}

	store := redisstore.New(rdb)
	svc := service.New(store, cfg.LinkTTL)
	limiter := ratelimit.New(store, cfg.RateLimit, cfg.RateWindow)

	handler := httpapi.NewHandler(svc, cfg.BaseURL, log)
	router := httpapi.NewRouter(handler, limiter, store, log)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()
	log.Info("listening", "addr", srv.Addr, "redis", cfg.RedisAddr)

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down", "timeout", cfg.ShutdownTimeout.String())
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("stopped")
	return nil
}
