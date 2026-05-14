package docker

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	daemonConfigPath        = "/etc/docker/daemon.json"
	podmanRegistriesPath    = "/etc/containers/registries.conf.d/999-pdai-registries.conf"
	podmanRegistriesComment = "# Managed by Pdai. Changes may be overwritten from the panel."
)

// DaemonConfig represents the managed subset of container runtime settings.
type DaemonConfig struct {
	RegistryMirrors    []string          `json:"registry-mirrors"`
	InsecureRegistries []string          `json:"insecure-registries"`
	LogDriver          string            `json:"log-driver"`
	LogOpts            map[string]string `json:"log-opts"`
	StorageDriver      string            `json:"storage-driver"`
	LiveRestore        *bool             `json:"live-restore"`
}

// ReadDaemonConfig reads /etc/docker/daemon.json and returns both a typed
// DaemonConfig (our managed fields) and the raw map (to preserve unmanaged fields).
func ReadDaemonConfig() (*DaemonConfig, map[string]interface{}, error) {
	raw := make(map[string]interface{})
	cfg := &DaemonConfig{}

	data, err := os.ReadFile(daemonConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, raw, nil
		}
		return nil, nil, fmt.Errorf("read daemon.json: %w", err)
	}

	if len(data) == 0 {
		return cfg, raw, nil
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse daemon.json: %w", err)
	}

	// Re-unmarshal into typed struct to extract managed fields.
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, nil, fmt.Errorf("parse daemon config fields: %w", err)
	}

	return cfg, raw, nil
}

// WriteDaemonConfig merges the managed DaemonConfig fields into the raw map
// (preserving any unmanaged fields) and writes back to /etc/docker/daemon.json.
func WriteDaemonConfig(cfg *DaemonConfig, raw map[string]interface{}) error {
	if raw == nil {
		raw = make(map[string]interface{})
	}

	// Merge managed fields into raw map.
	// For arrays: set if non-empty, delete if empty.
	if len(cfg.RegistryMirrors) > 0 {
		raw["registry-mirrors"] = cfg.RegistryMirrors
	} else {
		delete(raw, "registry-mirrors")
	}

	if len(cfg.InsecureRegistries) > 0 {
		raw["insecure-registries"] = cfg.InsecureRegistries
	} else {
		delete(raw, "insecure-registries")
	}

	if cfg.LogDriver != "" {
		raw["log-driver"] = cfg.LogDriver
	} else {
		delete(raw, "log-driver")
	}

	if len(cfg.LogOpts) > 0 {
		raw["log-opts"] = cfg.LogOpts
	} else {
		delete(raw, "log-opts")
	}

	if cfg.StorageDriver != "" {
		raw["storage-driver"] = cfg.StorageDriver
	} else {
		delete(raw, "storage-driver")
	}

	if cfg.LiveRestore != nil {
		raw["live-restore"] = *cfg.LiveRestore
	} else {
		delete(raw, "live-restore")
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daemon.json: %w", err)
	}
	data = append(data, '\n')

	// Ensure /etc/docker/ directory exists.
	if err := os.MkdirAll(filepath.Dir(daemonConfigPath), 0755); err != nil {
		return fmt.Errorf("create docker config dir: %w", err)
	}

	// Atomic write: write to .tmp then rename.
	tmpPath := daemonConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write daemon.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, daemonConfigPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename daemon.json.tmp: %w", err)
	}

	return nil
}

// WriteDaemonConfigRaw writes raw bytes to the daemon.json file atomically.
// Used for rollback when a restart with new config fails.
func WriteDaemonConfigRaw(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(daemonConfigPath), 0755); err != nil {
		return fmt.Errorf("create docker config dir: %w", err)
	}
	tmpPath := daemonConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write daemon.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, daemonConfigPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename daemon.json.tmp: %w", err)
	}
	return nil
}

// ReadPodmanConfig reads the Pdai-managed Podman registries drop-in.
func ReadPodmanConfig() (*DaemonConfig, error) {
	cfg := &DaemonConfig{}
	data, err := os.ReadFile(podmanRegistriesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read podman registries config: %w", err)
	}

	var currentLocation string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[[registry]]"):
			currentLocation = ""
		case strings.HasPrefix(line, "unqualified-search-registries"):
			cfg.RegistryMirrors = parseTomlStringList(line)
		case strings.HasPrefix(line, "location ="):
			currentLocation = parseTomlLocation(line)
		case strings.HasPrefix(line, "insecure = true") && currentLocation != "":
			cfg.InsecureRegistries = append(cfg.InsecureRegistries, currentLocation)
		}
	}
	return cfg, nil
}

// WritePodmanConfig writes a Podman registries.conf drop-in for fields that
// map cleanly from the existing UI: search registries and insecure registries.
func WritePodmanConfig(cfg *DaemonConfig) error {
	if err := validatePodmanRegistries(cfg.RegistryMirrors); err != nil {
		return err
	}
	if err := validatePodmanRegistries(cfg.InsecureRegistries); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString(podmanRegistriesComment)
	b.WriteByte('\n')
	if len(cfg.RegistryMirrors) > 0 {
		b.WriteString("unqualified-search-registries = ")
		b.WriteString(formatTomlStringList(cfg.RegistryMirrors))
		b.WriteString("\n\n")
	}
	for _, registry := range cfg.InsecureRegistries {
		registry = strings.TrimSpace(registry)
		if registry == "" {
			continue
		}
		b.WriteString("[[registry]]\n")
		b.WriteString("location = ")
		b.WriteString(formatTomlString(registry))
		b.WriteString("\ninsecure = true\n\n")
	}

	if err := os.MkdirAll(filepath.Dir(podmanRegistriesPath), 0755); err != nil {
		return fmt.Errorf("create podman config dir: %w", err)
	}
	tmpPath := podmanRegistriesPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("write podman registries config tmp: %w", err)
	}
	if err := os.Rename(tmpPath, podmanRegistriesPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename podman registries config tmp: %w", err)
	}
	return nil
}

func parseTomlStringList(line string) []string {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(line[idx+1:])), &values); err != nil {
		return nil
	}
	return values
}

func parseTomlLocation(line string) string {
	idx := strings.Index(line, "location =")
	if idx < 0 {
		return ""
	}
	part := strings.TrimSpace(line[idx+len("location ="):])
	if next := strings.Index(part, " "); next >= 0 {
		part = part[:next]
	}
	var location string
	if err := json.Unmarshal([]byte(part), &location); err != nil {
		return ""
	}
	return location
}

func formatTomlStringList(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

func formatTomlString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func validatePodmanRegistries(registries []string) error {
	for _, registry := range registries {
		registry = strings.TrimSpace(registry)
		if registry == "" {
			continue
		}
		if strings.ContainsAny(registry, "\r\n\t[]{}\"") {
			return fmt.Errorf("invalid registry value: %q", registry)
		}
		if parsed, err := url.Parse(registry); err == nil && parsed.Scheme != "" {
			if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("invalid registry value: %q", registry)
			}
		}
	}
	return nil
}

// ErrDaemonConfigNotSupportedOnPodman is returned when a Docker-only daemon
// setting is applied while Podman is the active runtime.
var ErrDaemonConfigNotSupportedOnPodman = fmt.Errorf("daemon.json is Docker-specific and has no effect under Podman")

// RestartDockerDaemon restarts the container runtime so daemon.json changes
// take effect. Docker hosts: standard `systemctl restart docker`. Podman
// hosts: refuse with ErrDaemonConfigNotSupportedOnPodman — the daemon.json
// UI has no effect on Podman, and pretending otherwise makes the panel
// lie to the admin about the applied config. Callers should check for the
// sentinel error and render a Podman-specific explanation.
//
// When the runtime is Unknown (neither binary present), returns a generic
// error so the admin can install a runtime via the install flow.
func RestartDockerDaemon() error {
	switch DetectRuntime() {
	case RuntimeDocker:
		// Legacy path: only reachable on hosts where Podman is not installed
		// but the `docker` CLI is. v0.12+ users always hit the RuntimePodman
		// branch below. See docs/08-podman-docker-shim-future.md for the
		// long-term plan.
		if _, err := exec.LookPath("rc-service"); err == nil {
			return exec.Command("rc-service", "docker", "restart").Run()
		}
		return exec.Command("systemctl", "restart", "docker").Run()
	case RuntimePodman:
		return ErrDaemonConfigNotSupportedOnPodman
	default:
		return fmt.Errorf("no container runtime detected; cannot restart daemon")
	}
}
