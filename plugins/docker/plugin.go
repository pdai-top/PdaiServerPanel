package docker

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pdai/pdai/internal/execx"
	pluginpkg "github.com/pdai/pdai/internal/plugin"
)

// Plugin implements the plugin.Plugin interface for Docker management.
//
// stateMu guards the mutable runtime-reachability fields (client,
// dockerAvailable, dockerError) that are read by requireDocker from every
// request goroutine and written by tryReconnect / dockerStatus when the
// daemon appears/disappears. Without the lock these fields were racing
// (go-review Group A finding). The handler client pointer that mirrors
// p.client is updated inside the same critical section.
type Plugin struct {
	client          *Client
	handler         *Handler
	dockerAvailable bool
	dockerError     string
	socketPath      string // configured docker socket path
	stateMu         sync.RWMutex
	// installMu serializes /api/plugins/docker/install so concurrent admin
	// clicks cannot launch overlapping EasyDocker subprocesses that would
	// both mutate /etc/docker/*, systemd units, and the package database.
	installMu sync.Mutex
}

// New creates a new Docker plugin instance.
func New() *Plugin {
	return &Plugin{}
}

// Metadata returns the plugin metadata.
func (p *Plugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		ID:          "docker",
		Name:        "Docker",
		Version:     "1.0.0",
		Description: "Podman-first container and Compose management with simple and advanced modes",
		Author:      "HaisPalen",
		Priority:    10,
		Icon:        "Container",
		Category:    "deploy",
	}
}

// Init initialises the Docker plugin: connects to Docker, migrates DB, registers routes.
// If Docker is not available, the plugin still initialises with limited functionality
// so that its routes exist for the guard middleware and status endpoint.
func (p *Plugin) Init(ctx *pluginpkg.Context) error {
	// Read optional socket path from plugin config. When empty, NewClient
	// resolves DOCKER_HOST / CONTAINER_HOST or a runtime-aware default.
	p.socketPath = ctx.ConfigStore.Get("socket_path")

	// Connect to the container runtime. NewClient only builds the SDK client;
	// we still need a live Ping() before exposing runtime-backed routes.
	client, err := NewClient(p.socketPath)
	if err != nil {
		p.stateMu.Lock()
		p.dockerAvailable = false
		p.dockerError = err.Error()
		p.stateMu.Unlock()
		ctx.Logger.Warn("Docker not available, plugin will run in limited mode", "err", err)
	} else {
		pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pingErr := client.Ping(pingCtx)
		cancel()
		if pingErr != nil {
			client.Close()
			p.stateMu.Lock()
			p.dockerAvailable = false
			p.dockerError = pingErr.Error()
			p.stateMu.Unlock()
			ctx.Logger.Warn("Docker daemon unreachable, plugin will run in limited mode", "err", pingErr)
			client = nil
		} else {
			p.stateMu.Lock()
			p.dockerAvailable = true
			p.dockerError = ""
			p.stateMu.Unlock()
			p.client = client
		}
	}

	// Migrate models.
	// Create handler (may use nil client, handler checks dockerAvailable).
	p.handler = NewHandler(client)
	p.handler.reconnectFn = p.tryReconnect

	// Register API routes under /api/plugins/docker/
	r := ctx.Router      // read-only
	a := ctx.AdminRouter // admin-only

	// Docker status endpoint (always available, even if Docker is not installed).
	r.GET("/status", p.dockerStatus)

	// Docker install endpoint (admin only, SSE streaming).
	a.POST("/install", p.installDocker)
	a.POST("/switch-podman", p.switchToPodman)

	// Daemon configuration (settings page — no requireDocker, settings should
	// be accessible even if the daemon is currently down).
	a.GET("/daemon-config", p.handler.GetDaemonConfig) // admin only — daemon config is sensitive
	a.PUT("/daemon-config", p.handler.UpdateDaemonConfig)

	// System (read)
	r.GET("/info", p.requireDocker(), p.handler.Info)

	o := ctx.OperatorRouter // operator+ (operational actions)

	// Containers (read + operator operations + admin mutations)
	r.GET("/containers", p.requireDocker(), p.handler.ListContainers)
	r.GET("/containers/:id", p.requireDocker(), p.handler.GetContainer)
	a.POST("/containers/run", p.requireDocker(), p.handler.RunContainer)
	a.POST("/containers/run/stream", p.requireDocker(), p.handler.RunContainerStream)
	a.PUT("/containers/:id", p.requireDocker(), p.handler.UpdateContainer)
	o.POST("/containers/:id/start", p.requireDocker(), p.handler.StartContainer)
	o.POST("/containers/:id/stop", p.requireDocker(), p.handler.StopContainer)
	o.POST("/containers/:id/restart", p.requireDocker(), p.handler.RestartContainer)
	a.DELETE("/containers/:id", p.requireDocker(), p.handler.RemoveContainer)
	// Container logs can leak secrets (Group A authz).
	o.GET("/containers/:id/logs", p.requireDocker(), p.handler.ContainerLogs)
	r.GET("/containers/:id/stats", p.requireDocker(), p.handler.ContainerStats)

	// Images (read + admin mutations)
	r.GET("/images", p.requireDocker(), p.handler.ListImages)
	a.POST("/images/pull", p.requireDocker(), p.handler.PullImage)
	a.DELETE("/images/:id", p.requireDocker(), p.handler.RemoveImage)
	a.POST("/images/prune", p.requireDocker(), p.handler.PruneImages)
	r.GET("/images/search", p.requireDocker(), p.handler.SearchImages)

	// Networks (read + admin mutations)
	r.GET("/networks", p.requireDocker(), p.handler.ListNetworks)
	a.POST("/networks", p.requireDocker(), p.handler.CreateNetwork)
	a.DELETE("/networks/:id", p.requireDocker(), p.handler.RemoveNetwork)

	// Volumes (read + admin mutations)
	r.GET("/volumes", p.requireDocker(), p.handler.ListVolumes)
	a.POST("/volumes", p.requireDocker(), p.handler.CreateVolume)
	a.DELETE("/volumes/:id", p.requireDocker(), p.handler.RemoveVolume)

	// WebSocket log streaming — operator tier (same rationale as HTTP logs).
	o.GET("/containers/:id/logs/ws", p.requireDocker(), p.handler.ContainerLogsWS)

	ctx.Logger.Info("Docker plugin routes registered", "docker_available", p.dockerAvailable)
	return nil
}

// requireDocker returns middleware that blocks requests if Docker is not available.
func (p *Plugin) requireDocker() gin.HandlerFunc {
	return func(c *gin.Context) {
		p.stateMu.RLock()
		available := p.dockerAvailable
		detail := p.dockerError
		p.stateMu.RUnlock()
		if !available {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":     "Docker is not available",
				"installed": false,
				"detail":    detail,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// tryReconnect attempts to connect to the Docker daemon and update the plugin state.
// Returns true if the daemon is reachable. The old client is closed AFTER
// the write lock is released so the ping goroutine holding it can finish
// cleanly (holding stateMu during Close() would let a waiting RLock queue
// observe an open-but-unreachable client and mis-route requests).
func (p *Plugin) tryReconnect() bool {
	client, err := NewClient(p.socketPath)
	if err != nil {
		p.stateMu.Lock()
		p.dockerAvailable = false
		p.dockerError = err.Error()
		p.stateMu.Unlock()
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		client.Close()
		p.stateMu.Lock()
		p.dockerAvailable = false
		p.dockerError = err.Error()
		p.stateMu.Unlock()
		return false
	}

	p.stateMu.Lock()
	oldClient := p.client
	p.client = client
	p.dockerAvailable = true
	p.dockerError = ""
	if p.handler != nil {
		p.handler.client = client
	}
	p.stateMu.Unlock()

	if oldClient != nil {
		oldClient.Close()
	}
	return true
}

// dockerStatus returns the current Docker availability status.
// It performs a live check: if the cached state says Docker is unavailable
// but the binary is installed, it tries to reconnect.
func (p *Plugin) dockerStatus(c *gin.Context) {
	installed := false
	version := ""

	// Check if a container runtime is reachable via the docker CLI. Under
	// v0.12 this is Podman via the podman-docker shim; detecting Podman
	// directly lets us surface accurate version strings without shelling
	// out twice.
	if DetectRuntime() != RuntimeUnknown {
		installed = true
		version = RuntimeVersion()
	} else if path, err := exec.LookPath("docker"); err == nil && path != "" {
		// Legacy fallback: unknown runtime but docker CLI exists (e.g. a
		// third-party wrapper). Keep the original behaviour.
		installed = true
		if out, err := exec.Command("docker", "--version").Output(); err == nil {
			version = strings.TrimSpace(string(out))
		}
	}

	p.stateMu.RLock()
	daemonRunning := p.dockerAvailable
	client := p.client
	p.stateMu.RUnlock()

	// Live check: if Docker binary is present but cached state says not available,
	// try to reconnect now. This handles the case where Docker was installed
	// after the plugin started.
	if installed && !daemonRunning {
		daemonRunning = p.tryReconnect()
	}

	// Also verify that a cached "available" state is still valid.
	if daemonRunning && client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := client.Ping(ctx); err != nil {
			daemonRunning = false
			p.stateMu.Lock()
			p.dockerAvailable = false
			p.dockerError = err.Error()
			p.stateMu.Unlock()
		}
	}

	p.stateMu.RLock()
	errMsg := ""
	if !daemonRunning {
		errMsg = p.dockerError
	}
	p.stateMu.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"installed":      installed,
		"daemon_running": daemonRunning,
		"version":        version,
		"error":          errMsg,
		"runtime":        DetectRuntime().String(),
	})
}

// installDocker performs the one-click Podman installation flow and streams
// the output to the client via SSE.
//
// Concurrency: installMu serializes concurrent admin clicks. Without it, two
// tabs firing /install in parallel would each start their own package-manager
// transaction on the same host — a reliable way to corrupt system state. We
// try-lock so the second caller gets a clear error instead of blocking on the
// first installer's runtime.
//
// Lifetime: the subprocess is tied to the request context via CommandContext
// so an SSE client disconnect (browser tab closed mid-install) kills the
// child process tree. The prior exec.Command variant let installers keep
// running after the socket closed, masking failure modes and wasting host
// resources if the admin clicked "retry" repeatedly.
func (p *Plugin) installDocker(c *gin.Context) {
	// Parse optional mirror parameter.
	var req struct {
		Mirror string `json:"mirror"` // none | public
	}
	_ = c.ShouldBindJSON(&req)

	// Set SSE headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	writeSSE := func(data string) {
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}

	writeEvent := func(event, data string) {
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
		c.Writer.Flush()
	}

	// Non-blocking install lock. If another admin is mid-install we refuse
	// rather than queue — a queued install behind the first would execute
	// against mutated host state and yield unpredictable results.
	if !p.installMu.TryLock() {
		writeSSE("Another install is already in progress.")
		writeEvent("error", "install already in progress")
		return
	}
	defer p.installMu.Unlock()

	// Quick check: if the runtime is already installed and reachable, skip
	// the EasyDocker script entirely. Under v0.12 Podman is pre-installed
	// by install.sh, so this is the normal path.
	if p.checkDockerAlreadyReady(writeSSE, writeEvent) {
		return
	}

	// EasyDocker only makes sense when real Docker is the target runtime.
	// If Podman is detected but unreachable we treat it as a configuration
	// issue (socket not started, permissions) rather than something fixable
	// by re-running Docker's installer.
	if DetectRuntime() == RuntimePodman {
		writeSSE("Podman is installed but the plugin could not reach its socket.")
		writeSSE("systemd: check 'systemctl status podman.socket'")
		writeSSE("OpenRC: check 'rc-service podman status' and '/run/podman/podman.sock'")
		writeSSE("Check: groups $USER (HaisPalen needs access to the 'podman' group/socket)")
		writeEvent("error", "podman installed but unreachable — refusing to run Docker installer under Podman runtime")
		return
	}

	// The old EasyDocker download path is gone. One-click install now performs
	// a local Podman package installation so the UI no longer depends on a
	// remote script that can 404.
	writeSSE("Installing Podman...")

	if req.Mirror != "" && req.Mirror != "public" {
		writeEvent("error", fmt.Sprintf("unsupported mirror value: %q (allowed: \"public\" or empty)", req.Mirror))
		return
	}

	installerCmd := `set -eu
(set -o pipefail) 2>/dev/null || true
log() { printf '%s\n' "$*"; }

if [ "$(id -u)" -ne 0 ]; then
  log "ERROR: installing Podman requires root privileges"
  exit 1
fi

if [ -r /etc/os-release ]; then
  . /etc/os-release
else
  log "ERROR: /etc/os-release not found"
  exit 1
fi

ids=" ${ID:-} ${ID_LIKE:-} "
os_family=""
case "$ids" in
  *debian*|*ubuntu*) os_family="debian" ;;
  *alpine*) os_family="alpine" ;;
  *rhel*|*fedora*|*centos*|*rocky*|*almalinux*|*ol*|*amzn*|*redhat*) os_family="el" ;;
esac
if [ -z "$os_family" ]; then
  log "ERROR: unsupported OS for automatic Podman installation: ${PRETTY_NAME:-unknown}"
  exit 1
fi

log "Installing Podman packages..."
case "$os_family" in
  debian)
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq podman podman-compose podman-docker uidmap slirp4netns fuse-overlayfs >/dev/null
    ;;
  el)
    if command -v dnf >/dev/null 2>&1; then
      dnf install -y podman podman-compose podman-docker slirp4netns fuse-overlayfs uidmap shadow-utils >/dev/null
    else
      yum install -y podman podman-compose podman-docker slirp4netns fuse-overlayfs uidmap shadow-utils >/dev/null
    fi
    ;;
  alpine)
    apk add --no-cache podman podman-compose podman-docker podman-openrc fuse-overlayfs slirp4netns shadow >/dev/null 2>&1 || \
      apk add --no-cache podman podman-compose fuse-overlayfs slirp4netns shadow >/dev/null
    ;;
esac

if ! command -v docker >/dev/null 2>&1 && command -v podman >/dev/null 2>&1; then
  ln -sf "$(command -v podman)" /usr/local/bin/docker
fi

log "Configuring Podman socket..."
if ! getent group podman >/dev/null 2>&1; then
  if command -v groupadd >/dev/null 2>&1; then
    groupadd -r podman
  elif command -v addgroup >/dev/null 2>&1; then
    addgroup -S podman
  fi
fi

install -d -m 755 /etc/containers
: > /etc/containers/nodocker
install -d -m 755 /etc/containers/registries.conf.d
cat > /etc/containers/registries.conf.d/999-pdai-shortnames.conf <<'REGCONF'
unqualified-search-registries = ['docker.io']
short-name-mode = 'permissive'
REGCONF

if [ -d /run/systemd/system ]; then
  install -d -m 755 /etc/systemd/system/podman.socket.d
  cat > /etc/systemd/system/podman.socket.d/10-pdai-group.conf <<'SOCKCONF'
[Socket]
SocketMode=0660
SocketGroup=podman
SOCKCONF
  systemctl daemon-reload
  systemctl enable podman.socket
  systemctl restart podman.socket
  for _ in $(seq 1 10); do
    [ -S /run/podman/podman.sock ] && break
    sleep 1
  done
  [ -S /run/podman/podman.sock ] || { log "ERROR: podman.socket failed to start"; exit 1; }
elif [ "$os_family" = "alpine" ] && command -v rc-service >/dev/null 2>&1; then
  rc-update add podman default 2>/dev/null || true
  rc-service podman start 2>/dev/null || true
else
  log "No supported service manager detected; Podman packages installed, but socket startup was skipped."
fi

install -d -m 755 /var/run
if [ -L /var/run/docker.sock ]; then
  rm -f /var/run/docker.sock
elif [ -S /var/run/docker.sock ]; then
  rm -f /var/run/docker.sock
elif [ -e /var/run/docker.sock ]; then
  log "ERROR: /var/run/docker.sock exists and is not a symlink/socket; refusing to overwrite it"
  exit 1
fi
ln -sf /run/podman/podman.sock /var/run/docker.sock

podman_ver=$(podman version --format '{{.Version}}' 2>/dev/null || podman --version 2>/dev/null || echo unknown)
log "Podman installed: ${podman_ver}"
if command -v docker >/dev/null 2>&1; then
  docker version >/dev/null 2>&1 || log "WARNING: docker compatibility command is installed but version check failed"
fi
`
	cmd := execx.ShellContext(c.Request.Context(), installerCmd)

	// Bind the subprocess lifetime to the HTTP request context so SSE client
	// disconnect terminates the whole installer tree. execx.CommandContext sets
	// Setpgid and installs a Cancel hook that SIGKILLs the process group so
	// children don't orphan to init.
	// Merge stdout and stderr.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeSSE("ERROR: " + err.Error())
		writeEvent("error", err.Error())
		return
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		writeSSE("ERROR: " + err.Error())
		writeEvent("error", err.Error())
		return
	}

	// Stream output line by line.
	rebootDetected := false
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		// Strip ANSI color codes for cleaner SSE output.
		clean := stripANSI(line)
		if clean != "" {
			// Detect reboot signal from the install script.
			if strings.Contains(strings.ToLower(clean), "reboot") {
				rebootDetected = true
			}
			writeSSE(clean)
		}
	}

	if err := cmd.Wait(); err != nil {
		// If a reboot was detected, this is expected — the script triggers a reboot
		// which kills the process. This is not a real failure.
		if rebootDetected {
			writeSSE("Server is rebooting to load new kernel modules...")
			writeEvent("reboot", "ok")
			return
		}
		writeSSE("ERROR: Installation failed: " + err.Error())
		writeEvent("error", "Installation failed: "+err.Error())
		return
	}

	writeSSE("Podman installation completed successfully!")

	// Invalidate the PATH-based runtime cache: the installer just provisioned
	// a binary that DetectRuntime would otherwise keep reporting as missing
	// until process restart.
	ResetRuntimeCache()

	// Try to reconnect after installation.
	if p.tryReconnect() {
		writeSSE("Podman daemon connected successfully!")
	} else {
		writeSSE("Podman installed but daemon connection pending. Please start Podman if needed.")
	}

	writeEvent("done", "ok")
}

// checkDockerAlreadyReady reports whether a usable container runtime is
// already reachable and returns true so the caller skips running the
// EasyDocker installer. The readiness check is runtime-aware:
//   - Podman (v0.12 default): require the `podman` binary + the Go SDK
//     reaching the socket. The `docker` CLI and `docker compose` are
//     NOT required because users with `podman-docker` removed (or a
//     fresh admin who only ran `dnf install podman`) still have a
//     working runtime.
//   - Docker (legacy): require `docker` binary + `docker compose version`
//   - SDK connection, matching pre-Phase-2 behaviour.
//   - Unknown: fall through to the caller, which will decide whether
//     the EasyDocker installer is appropriate.
//
// If the runtime is detected but the socket is unreachable, a bounded
// retry loop attempts to start the runtime's systemd unit and waits
// with exponential backoff so a transient socket flap doesn't become
// a hard user-visible failure. Only after all retries are exhausted
// does the function return false.
func (p *Plugin) checkDockerAlreadyReady(writeSSE func(string), writeEvent func(string, string)) bool {
	runtime := DetectRuntime()

	// Runtime-specific binary / CLI check
	switch runtime {
	case RuntimePodman:
		// podman binary is the authoritative marker — podman-docker shim is
		// optional (compose operations go through podman-compose directly
		// or through the shim-forwarded `docker compose` when present).
		if _, err := exec.LookPath("podman"); err != nil {
			return false
		}
	case RuntimeDocker:
		// docker binary + compose plugin both required for compose-based operations.
		if _, err := exec.LookPath("docker"); err != nil {
			return false
		}
		if _, err := exec.Command("docker", "compose", "version").CombinedOutput(); err != nil {
			return false
		}
	case RuntimeUnknown:
		return false
	}

	// Daemon / socket reachability check via Go SDK
	if !p.tryReconnect() {
		// Runtime binary exists but socket isn't reachable — try bounded
		// retries with exponential backoff. Covers podman.socket / docker
		// startup lag and transient socket restarts after drop-in changes.
		unit := runtime.SystemdUnit()
		if unit == "" {
			return false
		}
		writeSSE(fmt.Sprintf("%s is installed but socket is not reachable. Attempting to start %s ...",
			runtime, unit))

		backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
		var lastStartErr error
		for attempt, delay := range backoffs {
			var out []byte
			var startErr error
			if _, rcErr := exec.LookPath("rc-service"); rcErr == nil {
				serviceName := "docker"
				if unit == "podman.socket" {
					serviceName = "podman"
				}
				out, startErr = exec.Command("rc-service", serviceName, "start").CombinedOutput()
				if startErr == nil {
					out, startErr = exec.Command("rc-service", serviceName, "status").CombinedOutput()
				}
			} else {
				out, startErr = exec.Command("systemctl", "start", unit).CombinedOutput()
			}
			if startErr != nil {
				lastStartErr = fmt.Errorf("start %s: %s: %w", unit, strings.TrimSpace(string(out)), startErr)
			}
			time.Sleep(delay)
			if p.tryReconnect() {
				writeSSE(fmt.Sprintf("%s socket reachable after %d attempt(s)", runtime, attempt+1))
				goto connected
			}
		}
		if lastStartErr != nil {
			writeSSE(fmt.Sprintf("could not start %s: %v", unit, lastStartErr))
		}
		return false
	}
connected:

	// Best-effort runtime + compose version lines for the SSE stream.
	// Missing compose is a soft failure — app-store managed services still work if
	// only one of the compose implementations is present.
	runtimeVer := RuntimeVersion()
	composeOut, _ := exec.Command("docker", "compose", "version").CombinedOutput()
	composeVer := strings.TrimSpace(string(composeOut))

	writeSSE(fmt.Sprintf("%s is already installed and running!", runtime))
	if runtimeVer != "" {
		writeSSE(fmt.Sprintf("  %s", runtimeVer))
	}
	if composeVer != "" {
		writeSSE(fmt.Sprintf("  %s", composeVer))
	}
	writeSSE("No installation needed.")
	writeEvent("done", "ok")
	return true
}

// switchToPodman removes an empty Docker runtime and installs/configures Podman.
func (p *Plugin) switchToPodman(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	writeSSE := func(data string) {
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}
	writeEvent := func(event, data string) {
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
		c.Writer.Flush()
	}

	if !p.installMu.TryLock() {
		writeSSE("Another runtime installation or switch is already in progress.")
		writeEvent("error", "runtime operation already in progress")
		return
	}
	defer p.installMu.Unlock()

	if DetectRuntime() != RuntimeDocker {
		writeEvent("error", "current runtime is not Docker")
		return
	}
	if !p.tryReconnect() {
		writeEvent("error", "Docker is installed but its daemon is not reachable")
		return
	}

	p.stateMu.RLock()
	client := p.client
	p.stateMu.RUnlock()
	if client == nil {
		writeEvent("error", "Docker client is not available")
		return
	}

	listCtx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	containers, err := client.ListContainers(listCtx, true)
	cancel()
	if err != nil {
		writeEvent("error", "failed to verify Docker container list: "+err.Error())
		return
	}
	if len(containers) > 0 {
		writeEvent("error", fmt.Sprintf("refusing to switch: %d Docker container(s) still exist", len(containers)))
		return
	}

	writeSSE("Verified: Docker is installed and no containers exist. Starting Podman migration...")

	script := `set -eu
(set -o pipefail) 2>/dev/null || true
log() { printf '%s\n' "$*"; }
run() { log "+ $*"; "$@"; }

if [ "$(id -u)" -ne 0 ]; then
  log "ERROR: switching runtimes requires root privileges"
  exit 1
fi

if [ -r /etc/os-release ]; then
  . /etc/os-release
else
  log "ERROR: /etc/os-release not found"
  exit 1
fi

ids=" ${ID:-} ${ID_LIKE:-} "
os_family=""
case "$ids" in
  *debian*|*ubuntu*) os_family="debian" ;;
  *alpine*) os_family="alpine" ;;
esac
if [ -z "$os_family" ]; then
  log "ERROR: unsupported OS for automatic Docker → Podman switch: ${PRETTY_NAME:-unknown}"
  exit 1
fi

log "Stopping Docker services..."
if command -v systemctl >/dev/null 2>&1; then
  systemctl stop docker docker.socket containerd 2>/dev/null || true
  systemctl disable docker docker.socket containerd 2>/dev/null || true
fi
if command -v rc-service >/dev/null 2>&1; then
  rc-service docker stop 2>/dev/null || true
  rc-service containerd stop 2>/dev/null || true
  rc-update del docker default 2>/dev/null || true
  rc-update del containerd default 2>/dev/null || true
fi

log "Removing Docker packages..."
case "$os_family" in
  debian)
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get purge -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin docker.io docker-doc docker-compose docker-compose-v2 containerd runc 2>/dev/null || true
    apt-get autoremove -y -qq 2>/dev/null || true
    apt-get install -y -qq podman podman-compose podman-docker uidmap slirp4netns fuse-overlayfs >/dev/null
    ;;
  alpine)
    apk del docker docker-cli docker-cli-compose docker-compose containerd runc 2>/dev/null || true
    apk add --no-cache podman podman-compose podman-docker podman-openrc fuse-overlayfs slirp4netns shadow >/dev/null 2>&1 || \
      apk add --no-cache podman podman-compose fuse-overlayfs slirp4netns shadow >/dev/null
    ;;
esac

if ! command -v docker >/dev/null 2>&1 && command -v podman >/dev/null 2>&1; then
  ln -sf "$(command -v podman)" /usr/local/bin/docker
fi

log "Configuring Podman Docker-compatible socket..."
if ! getent group podman >/dev/null 2>&1; then
  if command -v groupadd >/dev/null 2>&1; then
    groupadd -r podman
  elif command -v addgroup >/dev/null 2>&1; then
    addgroup -S podman
  fi
fi

install -d -m 755 /etc/containers
: > /etc/containers/nodocker
install -d -m 755 /etc/containers/registries.conf.d
cat > /etc/containers/registries.conf.d/999-pdai-shortnames.conf <<'REGCONF'
unqualified-search-registries = ['docker.io']
short-name-mode = 'permissive'
REGCONF

if [ -d /run/systemd/system ]; then
  install -d -m 755 /etc/systemd/system/podman.socket.d
  cat > /etc/systemd/system/podman.socket.d/10-pdai-group.conf <<'SOCKCONF'
[Socket]
SocketMode=0660
SocketGroup=podman
SOCKCONF
  systemctl daemon-reload
  systemctl enable podman.socket
  systemctl restart podman.socket
  for _ in $(seq 1 10); do
    [ -S /run/podman/podman.sock ] && break
    sleep 1
  done
  [ -S /run/podman/podman.sock ] || { log "ERROR: podman.socket failed to start"; exit 1; }
elif [ "$os_family" = "alpine" ] && command -v rc-service >/dev/null 2>&1; then
  rc-update add podman default 2>/dev/null || true
  rc-service podman start 2>/dev/null || true
else
  log "No supported service manager detected; Podman packages installed, but socket startup was skipped."
fi

install -d -m 755 /var/run
if [ -L /var/run/docker.sock ]; then
  rm -f /var/run/docker.sock
elif [ -S /var/run/docker.sock ]; then
  rm -f /var/run/docker.sock
elif [ -e /var/run/docker.sock ]; then
  log "ERROR: /var/run/docker.sock exists and is not a symlink/socket; refusing to overwrite it"
  exit 1
fi
ln -sf /run/podman/podman.sock /var/run/docker.sock

podman_ver=$(podman version --format '{{.Version}}' 2>/dev/null || podman --version 2>/dev/null || echo unknown)
log "Podman installed: ${podman_ver}"
if command -v docker >/dev/null 2>&1; then
  docker version >/dev/null 2>&1 || log "WARNING: docker compatibility command is installed but version check failed"
fi
log "Docker → Podman switch completed."
`

	cmd := execx.ShellContext(c.Request.Context(), script)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeEvent("error", err.Error())
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		writeEvent("error", err.Error())
		return
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if clean := stripANSI(scanner.Text()); clean != "" {
			writeSSE(clean)
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		writeSSE("WARNING: failed reading migration output: " + scanErr.Error())
	}
	if err := cmd.Wait(); err != nil {
		writeEvent("error", "Podman switch failed: "+err.Error())
		return
	}

	p.stateMu.Lock()
	oldClient := p.client
	p.client = nil
	p.dockerAvailable = false
	p.dockerError = "runtime switched to Podman; reconnect pending"
	if p.handler != nil {
		p.handler.client = nil
	}
	p.stateMu.Unlock()
	if oldClient != nil {
		oldClient.Close()
	}

	ResetRuntimeCache()
	if p.tryReconnect() {
		writeSSE("Podman socket connected successfully.")
	} else {
		writeSSE("Podman installed, but Pdai could not reconnect yet. Check podman.socket and retry status.")
	}
	writeEvent("done", "ok")
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm' or end of string.
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
			} else {
				i = j
			}
			continue
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// Start is called after Init. No background tasks needed yet.
func (p *Plugin) Start() error {
	return nil
}

// Stop closes the Docker client.
func (p *Plugin) Stop() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}

// FrontendManifest declares the frontend routes for the Docker plugin.
func (p *Plugin) FrontendManifest() pluginpkg.FrontendManifest {
	return pluginpkg.FrontendManifest{
		ID: "docker",
		Routes: []pluginpkg.FrontendRoute{
			{Path: "/docker", Component: "DockerOverview", Menu: true, Icon: "Container", Label: "Docker", LabelZh: "Docker"},
			{Path: "/docker/containers", Component: "DockerContainers", Label: "Containers", LabelZh: "容器管理"},
			{Path: "/docker/images", Component: "DockerImages", Label: "Images", LabelZh: "镜像管理"},
			{Path: "/docker/networks", Component: "DockerNetworks", Label: "Networks", LabelZh: "网络管理"},
			{Path: "/docker/volumes", Component: "DockerVolumes", Label: "Volumes", LabelZh: "存储卷"},
			{Path: "/docker/settings", Component: "DockerSettings", Label: "Settings", LabelZh: "设置"},
		},
		MenuGroup: "deploy",
		MenuOrder: 10,
	}
}
