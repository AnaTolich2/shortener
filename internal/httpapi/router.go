package httpapi

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

func NewRouter(h *Handler, limiter Limiter, pinger Pinger, log *slog.Logger) *gin.Engine {
	r := gin.New()

	_ = r.SetTrustedProxies(nil)

	r.Use(gin.Recovery(), requestLogger(log))

	r.GET("/healthz", health(pinger))
	r.GET("/r/:code", h.redirect)

	v1 := r.Group("/api/v1")
	v1.POST("/shorten", rateLimit(limiter, log), h.shorten)

	return r
}
