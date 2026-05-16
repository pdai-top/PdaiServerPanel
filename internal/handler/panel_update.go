package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pdai/pdai/internal/updater"
)

// PanelUpdateHandler exposes self-update endpoints for the panel binary.
type PanelUpdateHandler struct {
	manager        *updater.Manager
	shutdownServer func(context.Context) error
}

// NewPanelUpdateHandler creates a panel update handler.
func NewPanelUpdateHandler(manager *updater.Manager, shutdownServer func(context.Context) error) *PanelUpdateHandler {
	return &PanelUpdateHandler{manager: manager, shutdownServer: shutdownServer}
}

// Check returns latest release information.
func (h *PanelUpdateHandler) Check(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	info, err := h.manager.Check(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}

// Prepare downloads, verifies and stages the release package.
func (h *PanelUpdateHandler) Prepare(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	info, err := h.manager.Prepare(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "release": info})
		return
	}
	c.JSON(http.StatusOK, info)
}

// Restart starts the helper process and then shuts down the current HTTP server.
func (h *PanelUpdateHandler) Restart(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := h.manager.RestartWithHelper(ctx); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "update helper started; panel will restart shortly"})

	if h.shutdownServer != nil {
		go func() {
			time.Sleep(800 * time.Millisecond)
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()
			_ = h.shutdownServer(shutdownCtx)
		}()
	}
}
