package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AnaTolich2/shortener/internal/entity"
)

type Shortener interface {
	Shorten(ctx context.Context, rawURL string) (code string, err error)
	Resolve(ctx context.Context, code string) (rawURL string, err error)
}

type Pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	svc     Shortener
	baseURL string
	log     *slog.Logger
}

func NewHandler(svc Shortener, baseURL string, log *slog.Logger) *Handler {
	return &Handler{
		svc:     svc,
		baseURL: strings.TrimRight(baseURL, "/"),
		log:     log,
	}
}

type shortenRequest struct {
	URL string `json:"url" binding:"required,max=2048"`
}

type shortenResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(c *gin.Context, status int, msg string) {
	c.JSON(status, errorResponse{Error: msg})
}

func (h *Handler) shorten(c *gin.Context) {
	var req shortenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "request body must be JSON like {\"url\": \"https://example.com\"}")
		return
	}

	code, err := h.svc.Shorten(c.Request.Context(), req.URL)
	switch {
	case err == nil:
		c.JSON(http.StatusCreated, shortenResponse{
			Code:     code,
			ShortURL: h.baseURL + "/r/" + code,
		})
	case errors.Is(err, entity.ErrInvalidURL):
		writeError(c, http.StatusBadRequest, err.Error())
	default:
		h.log.Error("shorten failed", "error", err)
		writeError(c, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) redirect(c *gin.Context) {
	target, err := h.svc.Resolve(c.Request.Context(), c.Param("code"))
	switch {
	case err == nil:
		c.Redirect(http.StatusFound, target)
	case errors.Is(err, entity.ErrNotFound):
		writeError(c, http.StatusNotFound, "link not found")
	default:
		h.log.Error("resolve failed", "error", err)
		writeError(c, http.StatusInternalServerError, "internal error")
	}
}

func health(p Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()

		if err := p.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "redis unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
