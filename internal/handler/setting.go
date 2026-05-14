package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pdai/pdai/internal/model"
	"gorm.io/gorm"
)

// SettingHandler manages panel settings
type SettingHandler struct {
	db *gorm.DB
}

// NewSettingHandler creates a new SettingHandler
func NewSettingHandler(db *gorm.DB) *SettingHandler {
	return &SettingHandler{db: db}
}

// GetAll returns all settings as a key-value map
func (h *SettingHandler) GetAll(c *gin.Context) {
	var settings []model.Setting
	h.db.Find(&settings)
	result := make(map[string]string, len(settings))
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	c.JSON(http.StatusOK, gin.H{"settings": result})
}

// Update updates a setting by key
func (h *SettingHandler) Update(c *gin.Context) {
	// Value is *string so we can distinguish missing field from empty
	// string. wildcard_domain explicitly allows empty (= disable
	// preview deploys); other keys require a non-nil value (and most
	// reject empty-string too 鈥?see per-key validation below).
	// PB-R3-M1 fix: previously Value lost its `required` tag entirely,
	// which silently let `auto_reload=""` slip through and disable
	// the auto-reload feature.
	var req struct {
		Key   string  `json:"key" binding:"required"`
		Value *string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value is required"})
		return
	}
	value := *req.Value

	// Only allow known settings
	allowed := map[string]bool{
		"auto_reload":           true,
		"site_name":             true,
		"server_ipv4":           true,
		"server_ipv6":           true,
		"wildcard_domain":       true, // PB-R2-H2: required by Preview Deploy (v0.14+)
		"max_concurrent_builds": true, // v0.17-A1: panel-wide build concurrency cap
		"panel_autostart":       true,
		"startup_script":        true,
	}
	if !allowed[req.Key] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown setting: " + req.Key})
		return
	}

	// Per-key normalize + validate. Defense-in-depth against a UI that
	// bypassed the frontend regex.
	switch req.Key {
	case "site_name":
		value = strings.TrimSpace(value)
		if len([]rune(value)) > 80 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "site_name must be 80 characters or fewer"})
			return
		}
		if strings.ContainsAny(value, "\r\n\t") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "site_name cannot contain control characters"})
			return
		}
	case "wildcard_domain":
		// Empty is allowed (disables previews). Non-empty must be a
		// bare DNS suffix; defensively normalize before validating.
		if value != "" {
			v := strings.ToLower(strings.TrimSpace(value))
			if !validWildcardDomain(v) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wildcard_domain 鈥?must be a bare DNS suffix (e.g. preview.example.com)"})
				return
			}
			value = v
		}
	case "auto_reload", "panel_autostart":
		// Strict boolean string. Anything else (including "") could
		// silently flip the read-side default check.
		if value != "true" && value != "false" {
			c.JSON(http.StatusBadRequest, gin.H{"error": req.Key + " must be 'true' or 'false'"})
			return
		}
	case "max_concurrent_builds":
		// v0.17-A1: positive integer, capped at 64 (mirrors the
		// in-process cap in parseMaxConcurrentBuilds. Empty resets
		// to default 鈥?handle in the read-side, not here.
		if value != "" {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n <= 0 || n > 64 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "max_concurrent_builds must be an integer between 1 and 64"})
				return
			}
			value = strconv.Itoa(n)
		}
	case "startup_script":
		if len(value) > 20000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "startup_script is too long"})
			return
		}
	}

	if err := h.db.Where("key = ?", req.Key).Assign(model.Setting{Value: value}).FirstOrCreate(&model.Setting{Key: req.Key}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Key == "panel_autostart" {
		if runtime.GOOS != "linux" {
			c.JSON(http.StatusOK, gin.H{"message": "Setting updated"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()

		if hasSystemd() {
			var unitName string
			if value == "true" {
				var err error
				unitName, err = installPanelServiceUnit()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
			} else {
				unitName = detectPanelServiceUnit()
			}
			if unitName != "" {
				if output, err := exec.CommandContext(ctx, "systemctl", "daemon-reload").CombinedOutput(); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to reload systemd: %v %s", err, strings.TrimSpace(string(output)))})
					return
				}

				cmdName := "disable"
				if value == "true" {
					cmdName = "enable"
				}
				cmd := exec.CommandContext(ctx, "systemctl", cmdName, unitName)
				if output, err := cmd.CombinedOutput(); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to %s %s: %v %s", cmdName, unitName, err, strings.TrimSpace(string(output)))})
					return
				}
			}
		} else if hasOpenRC() {
			var serviceName string
			if value == "true" {
				var err error
				serviceName, err = installPanelOpenRCService()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
			} else {
				serviceName = detectPanelOpenRCService()
			}
			if serviceName != "" {
				cmdName := "del"
				if value == "true" {
					cmdName = "add"
				}
				cmd := exec.CommandContext(ctx, "rc-update", cmdName, serviceName, "default")
				if output, err := cmd.CombinedOutput(); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to rc-update %s %s: %v %s", cmdName, serviceName, err, strings.TrimSpace(string(output)))})
					return
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Setting updated"})
}

// validWildcardDomain matches a bare DNS suffix: at least two labels,
// each label `a-z0-9` with optional `-` (not at edges) AND 鈮?3 chars
// per RFC 1035, total 鈮?53. PB-R3-L2 fix.
var wildcardDomainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

func hasSystemd() bool {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return true
	}
	return false
}

func hasOpenRC() bool {
	if _, err := exec.LookPath("rc-service"); err != nil {
		return false
	}
	if _, err := exec.LookPath("rc-update"); err != nil {
		return false
	}
	return true
}

func detectPanelServiceUnit() string {
	if unit := strings.TrimSpace(os.Getenv("SYSTEMD_UNIT")); strings.HasSuffix(unit, ".service") {
		return unit
	}

	if unit := detectPanelServiceUnitByPID(os.Getpid()); unit != "" {
		return unit
	}

	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			for _, part := range strings.Split(line, "/") {
				unit := strings.TrimSpace(part)
				if strings.HasSuffix(unit, ".service") {
					return unit
				}
			}
		}
	}

	candidates := []string{
		"pdai.service",
		"Pdai.service",
		"pdai-panel.service",
		"PdaiPanel.service",
		"webcasa.service",
		"WebCasa.service",
		"haispalen.service",
		"HaisPalen.service",
	}
	searchPaths := []string{
		"/etc/systemd/system",
		"/lib/systemd/system",
		"/usr/lib/systemd/system",
	}
	for _, dir := range searchPaths {
		for _, name := range candidates {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return filepath.Base(path)
			}
		}
	}
	return ""
}

func detectPanelOpenRCService() string {
	candidates := []string{
		"pdai",
		"Pdai",
		"pdai-panel",
		"PdaiPanel",
		"webcasa",
		"WebCasa",
		"haispalen",
		"HaisPalen",
	}
	if exePath, err := os.Executable(); err == nil {
		if name := serviceScriptNameFromExecutable(exePath); name != "" {
			candidates = append([]string{name}, candidates...)
		}
	}
	for _, name := range candidates {
		if _, err := os.Stat(filepath.Join("/etc/init.d", name)); err == nil {
			return name
		}
	}
	return ""
}

func detectPanelServiceUnitByPID(pid int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, "systemctl", "show", "--property=Names", fmt.Sprintf("--pid=%d", pid)).CombinedOutput()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(output))
	line = strings.TrimPrefix(line, "Names=")
	for _, name := range strings.Fields(line) {
		if strings.HasSuffix(name, ".service") {
			return name
		}
	}
	return ""
}

func installPanelServiceUnit() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve current executable: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve executable symlink: %w", err)
	}
	if !filepath.IsAbs(exePath) {
		return "", fmt.Errorf("current executable path is not absolute: %s", exePath)
	}
	if info, err := os.Stat(exePath); err != nil || info.IsDir() {
		if err != nil {
			return "", fmt.Errorf("current executable is not accessible: %w", err)
		}
		return "", fmt.Errorf("current executable path is a directory: %s", exePath)
	}

	unitName := serviceUnitNameFromExecutable(exePath)
	unitPath := filepath.Join("/etc/systemd/system", unitName)
	workingDir := filepath.Dir(exePath)
	content := fmt.Sprintf(`[Unit]
Description=Pdai Panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=3
Environment=GIN_MODE=release

[Install]
WantedBy=multi-user.target
`, workingDir, exePath)

	if err := os.WriteFile(unitPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", unitPath, err)
	}
	return unitName, nil
}

func installPanelOpenRCService() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve current executable: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve executable symlink: %w", err)
	}
	if !filepath.IsAbs(exePath) {
		return "", fmt.Errorf("current executable path is not absolute: %s", exePath)
	}
	if info, err := os.Stat(exePath); err != nil || info.IsDir() {
		if err != nil {
			return "", fmt.Errorf("current executable is not accessible: %w", err)
		}
		return "", fmt.Errorf("current executable path is a directory: %s", exePath)
	}

	serviceName := serviceScriptNameFromExecutable(exePath)
	servicePath := filepath.Join("/etc/init.d", serviceName)
	content := fmt.Sprintf(`#!/sbin/openrc-run
description="Pdai Panel"
directory=%s
command=%s
command_background=true
pidfile="/run/%s.pid"

depend() {
	need net
}
`, shellSingleQuote(filepath.Dir(exePath)), shellSingleQuote(exePath), serviceName)

	if err := os.WriteFile(servicePath, []byte(content), 0755); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", servicePath, err)
	}
	return serviceName, nil
}

func serviceUnitNameFromExecutable(exePath string) string {
	name := strings.TrimSpace(filepath.Base(exePath))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "pdai.service"
	}

	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '@' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	unitName := strings.Trim(b.String(), ".-")
	if unitName == "" {
		unitName = "pdai"
	}
	if !strings.HasSuffix(unitName, ".service") {
		unitName += ".service"
	}
	return unitName
}

func serviceScriptNameFromExecutable(exePath string) string {
	name := strings.TrimSpace(filepath.Base(exePath))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "pdai"
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else if r == '.' {
			b.WriteByte('-')
		} else {
			b.WriteByte('-')
		}
	}
	serviceName := strings.Trim(b.String(), "-")
	if serviceName == "" {
		serviceName = "pdai"
	}
	return serviceName
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func validWildcardDomain(s string) bool {
	if len(s) > 253 {
		return false
	}
	if !wildcardDomainRE.MatchString(s) {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) > 63 {
			return false
		}
	}
	return true
}
