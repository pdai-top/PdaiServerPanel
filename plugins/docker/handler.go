package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pdai/pdai/internal/auth"
)

// Handler implements the REST API for Docker management.
type Handler struct {
	client      *Client
	reconnectFn func() bool // called after daemon restart to reconnect
}

// NewHandler creates a Docker Handler.
func NewHandler(client *Client) *Handler {
	return &Handler{client: client}
}

func (h *Handler) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// ── System ──

// Info returns Docker system info.
func (h *Handler) Info(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	info, err := h.client.Info(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, info)
}

func (h *Handler) ListContainers(c *gin.Context) {
	if h.client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Docker is not available"})
		return
	}

	ctx, cancel := h.ctx()
	defer cancel()
	all := c.DefaultQuery("all", "true") == "true"
	containers, err := h.client.ListContainers(ctx, all)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	// Bound the image-status resolution to a short window so a slow Docker
	// inspect cannot turn a list request into a 5s wait. Missing statuses
	// stay "unknown" and surface correctly on the next call once cached.
	annotateCtx, annotateCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	h.client.AnnotateImageStatuses(annotateCtx, containers)
	annotateCancel()
	c.JSON(http.StatusOK, gin.H{"containers": containers})
}

// GetContainer returns a single container's editable details.
func (h *Handler) GetContainer(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	detail, err := h.client.GetContainer(ctx, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// UpdateContainer recreates a standalone container with updated settings.
func (h *Handler) UpdateContainer(c *gin.Context) {
	var req RunContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	timeout := 30 * time.Second
	if req.ForcePullImage {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	id, err := h.client.UpdateContainer(ctx, c.Param("id"), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "message": "Container updated"})
}

// StartContainer starts a container.
func (h *Handler) StartContainer(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.StartContainer(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Container started"})
}

// StopContainer stops a container.
func (h *Handler) StopContainer(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.StopContainer(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Container stopped"})
}

// RestartContainer restarts a container.
func (h *Handler) RestartContainer(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RestartContainer(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Container restarted"})
}

// RemoveContainer removes a container.
func (h *Handler) RemoveContainer(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RemoveContainer(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Container removed"})
}

// ContainerLogs returns recent logs.
func (h *Handler) ContainerLogs(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tail := sanitizeTail(c.DefaultQuery("tail", "200"))
	reader, err := h.client.ContainerLogs(ctx, c.Param("id"), tail, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer reader.Close()

	data, _ := readAll(reader, 1<<20) // max 1MB
	c.JSON(http.StatusOK, gin.H{"logs": string(data)})
}

// ContainerStats returns resource stats.
func (h *Handler) ContainerStats(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	stats, err := h.client.GetContainerStats(ctx, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// ── Daemon Configuration ──

// GetDaemonConfig returns the current container runtime configuration.
func (h *Handler) GetDaemonConfig(c *gin.Context) {
	runtime := DetectRuntime()
	var cfg *DaemonConfig
	var err error
	if runtime == RuntimePodman {
		cfg, err = ReadPodmanConfig()
	} else {
		cfg, _, err = ReadDaemonConfig()
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"config":  cfg,
		"runtime": runtime.String(),
	})
}

// daemonConfigMu serializes concurrent /daemon-config writes so two admins
// (or a retry + a new click) cannot race while writing runtime config and
// restarting or reconnecting the active container runtime.
var daemonConfigMu sync.Mutex

// UpdateDaemonConfig writes the active runtime configuration.
func (h *Handler) UpdateDaemonConfig(c *gin.Context) {
	runtime := DetectRuntime()
	daemonConfigMu.Lock()
	defer daemonConfigMu.Unlock()
	var cfg DaemonConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if runtime == RuntimePodman {
		if err := WritePodmanConfig(&cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write Podman config: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":      "ok",
			"runtime":     "podman",
			"reconnected": true,
		})
		return
	}

	// Read existing raw config to preserve unmanaged fields.
	_, raw, err := ReadDaemonConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read current config: " + err.Error()})
		return
	}

	// Back up the current config as raw bytes so we can restore on restart failure.
	oldConfig, _ := os.ReadFile("/etc/docker/daemon.json")

	// Write merged config.
	if err := WriteDaemonConfig(&cfg, raw); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write config: " + err.Error()})
		return
	}

	// Restart Docker daemon. If it fails, the new config is likely invalid —
	// restore the previous config so Docker can start again.
	if err := RestartDockerDaemon(); err != nil {
		// Attempt to rollback.
		if rollbackErr := WriteDaemonConfigRaw(oldConfig); rollbackErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("restart failed: %v; rollback also failed: %v — manual intervention required", err, rollbackErr),
			})
			return
		}
		// Try restarting with the old config.
		_ = RestartDockerDaemon()
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid config: Docker failed to restart, previous config restored"})
		return
	}

	// Wait for daemon to come back (up to 15 seconds).
	reconnected := false
	if h.reconnectFn != nil {
		for i := 0; i < 15; i++ {
			time.Sleep(1 * time.Second)
			if h.reconnectFn() {
				reconnected = true
				break
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"reconnected": reconnected,
	})
}

// ── Run Container ──

// RunContainer creates and starts a standalone container.
func (h *Handler) RunContainer(c *gin.Context) {
	var req RunContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Image == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image is required"})
		return
	}

	// Validate restart policy.
	switch req.RestartPolicy {
	case "", "no", "always", "unless-stopped", "on-failure":
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid restart_policy, must be one of: no, always, unless-stopped, on-failure"})
		return
	}

	// Try to pull the image first, but don't fail if it errors — the image
	// may already exist locally (local build, offline environment, etc.).
	pullCtx, pullCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer pullCancel()
	if reader, err := h.client.PullImage(pullCtx, req.Image); err == nil {
		_, _ = io.Copy(io.Discard, reader)
		reader.Close()
	}

	// Create and start the container.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := h.client.RunContainer(ctx, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "message": "Container created and started"})
}

// ── Images ──

// RunContainerStream creates and starts a standalone container while streaming
// image-pull and create/start progress back to the browser via SSE.
func (h *Handler) RunContainerStream(c *gin.Context) {
	var req RunContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Image == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image is required"})
		return
	}
	switch req.RestartPolicy {
	case "", "no", "always", "unless-stopped", "on-failure":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid restart_policy, must be one of: no, always, unless-stopped, on-failure"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	writeEvent := func(event, data string) {
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
		c.Writer.Flush()
	}
	writeLog := func(data string) {
		writeEvent("log", data)
	}

	ctx := c.Request.Context()
	writeLog("Pulling image: " + req.Image)
	if reader, err := h.client.PullImage(ctx, req.Image); err == nil {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			writeLog(scanner.Text())
		}
		reader.Close()
		if err := scanner.Err(); err != nil {
			writeLog("Image pull log stream ended with warning: " + err.Error())
		}
	} else {
		writeLog("Image pull skipped or failed; continuing with local image if available: " + err.Error())
	}

	writeLog("Creating and starting container...")
	id, err := h.client.RunContainer(ctx, &req)
	if err != nil {
		writeEvent("error", err.Error())
		return
	}
	writeLog("Container created: " + id)
	writeEvent("done", id)
}

// ListImages returns all local images.
func (h *Handler) ListImages(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	images, err := h.client.ListImages(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	containers, err := h.client.ListContainers(ctx, true)
	if err == nil {
		used := make(map[string][]string)
		for _, ctr := range containers {
			imageID := strings.TrimSpace(ctr.ImageID)
			if imageID == "" {
				continue
			}
			used[imageID] = append(used[imageID], ctr.Name)
			if strings.HasPrefix(imageID, "sha256:") && len(imageID) >= 19 {
				used[imageID[7:19]] = append(used[imageID[7:19]], ctr.Name)
			}
		}
		for i := range images {
			if names := used[images[i].ImageID]; len(names) > 0 {
				images[i].Used = true
				images[i].UsedBy = names
			} else if names := used[images[i].ID]; len(names) > 0 {
				images[i].Used = true
				images[i].UsedBy = names
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"images": images})
}

// PullImage pulls an image.
func (h *Handler) PullImage(c *gin.Context) {
	var req struct {
		Image string `json:"image" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	reader, err := h.client.PullImage(ctx, req.Image)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer reader.Close()

	// Drain the pull output (we could stream it via SSE in the future).
	data, _ := readAll(reader, 1<<20)
	c.JSON(http.StatusOK, gin.H{"message": "Image pulled", "output": string(data)})
}

// RemoveImage removes an image.
func (h *Handler) RemoveImage(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RemoveImage(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Image removed"})
}

// PruneImages removes unused images.
func (h *Handler) PruneImages(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	reclaimed, err := h.client.PruneImages(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Images pruned", "space_reclaimed": reclaimed})
}

// ── Networks ──

// ListNetworks returns all networks.
func (h *Handler) ListNetworks(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	nets, err := h.client.ListNetworks(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"networks": nets})
}

// CreateNetwork creates a network.
func (h *Handler) CreateNetwork(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := h.ctx()
	defer cancel()
	id, err := h.client.CreateNetwork(ctx, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "message": "Network created"})
}

// RemoveNetwork removes a network.
func (h *Handler) RemoveNetwork(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RemoveNetwork(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Network removed"})
}

// ── Volumes ──

// ListVolumes returns all volumes.
func (h *Handler) ListVolumes(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	vols, err := h.client.ListVolumes(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"volumes": vols})
}

// CreateVolume creates a volume.
func (h *Handler) CreateVolume(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.CreateVolume(ctx, req.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Volume created"})
}

// RemoveVolume removes a volume.
func (h *Handler) RemoveVolume(c *gin.Context) {
	ctx, cancel := h.ctx()
	defer cancel()
	if err := h.client.RemoveVolume(ctx, c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Volume removed"})
}

// ── Helpers ──

// sanitizeTail validates the tail parameter as a positive integer with a max cap.
func sanitizeTail(s string) string {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return "200"
	}
	if n > 5000 {
		n = 5000
	}
	return strconv.Itoa(n)
}

func parseID(c *gin.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, err
	}
	return uint(id), nil
}

func readAll(r interface{ Read([]byte) (int, error) }, maxBytes int) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for len(buf) < maxBytes {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	if len(buf) > maxBytes {
		buf = buf[:maxBytes]
	}
	return buf, nil
}

// ── WebSocket Log Streaming ──

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

// ContainerLogsWS streams container logs via WebSocket.
func (h *Handler) ContainerLogsWS(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, auth.WSUpgradeResponseHeader(c))
	if err != nil {
		return
	}
	defer conn.Close()

	containerID := c.Param("id")
	tail := sanitizeTail(c.DefaultQuery("tail", "100"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start a goroutine to detect client disconnect
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	reader, err := h.client.ContainerLogs(ctx, containerID, tail, true)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Error: "+err.Error()))
		return
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		// Docker log stream has 8-byte header; strip it for clean output
		if len(line) > 8 {
			line = line[8:]
		}
		if err := conn.WriteMessage(websocket.TextMessage, line); err != nil {
			return
		}
	}
}

// ── Image Search ──

// SearchImages searches Docker Hub for images.
func (h *Handler) SearchImages(c *gin.Context) {
	term := c.Query("q")
	if term == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q parameter is required"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "25"))
	ctx, cancel := h.ctx()
	defer cancel()
	results, err := h.client.SearchImages(ctx, term, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}
