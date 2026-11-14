package httpapi

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type Limiter interface {
	Allow(ctx context.Context, id string) (allowed bool, retryAfter time.Duration, err error)
}

func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		log.Info("request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration", time.Since(start).String(),
		)
	}
}

func rateLimit(l Limiter, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, retryAfter, err := l.Allow(c.Request.Context(), c.ClientIP())
		if err != nil {
			log.Warn("rate limiter unavailable, failing open", "error", err)
			c.Next()
			return
		}
		if !allowed {
			seconds := int(math.Ceil(retryAfter.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, errorResponse{Error: "too many requests"})
			return
		}
		c.Next()
	}
}
