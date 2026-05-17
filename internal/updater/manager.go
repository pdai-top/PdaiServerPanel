package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const defaultReleaseAPI = "https://api.github.com/repos/pdai-top/PdaiServerPanel/releases/latest"

// ReleaseInfo is returned to the frontend for update prompts.
type ReleaseInfo struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	CanUpdate       bool   `json:"can_update"`
	Reason          string `json:"reason,omitempty"`
	ReleaseName     string `json:"release_name,omitempty"`
	TagName         string `json:"tag_name,omitempty"`
	PublishedAt     string `json:"published_at,omitempty"`
	Body            string `json:"body,omitempty"`
	HTMLURL         string `json:"html_url,omitempty"`
	AssetName       string `json:"asset_name,omitempty"`
	AssetURL        string `json:"asset_url,omitempty"`
	ChecksumURL     string `json:"checksum_url,omitempty"`
	Prepared        bool   `json:"prepared"`
	PreparedAt      string `json:"prepared_at,omitempty"`
	PreparedVersion string `json:"prepared_version,omitempty"`
	PreparedPath    string `json:"-"`
	RuntimeGOOS     string `json:"runtime_goos,omitempty"`
	RuntimeGOARCH   string `json:"runtime_goarch,omitempty"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
		ContentType        string `json:"content_type"`
	} `json:"assets"`
}

type preparedUpdate struct {
	Version   string
	AssetName string
	AssetPath string
	CreatedAt time.Time
}

// Manager checks, stages and restarts panel updates.
type Manager struct {
	currentVersion string
	releaseAPI     string
	dataDir        string
	client         *http.Client

	mu       sync.RWMutex
	prepared *preparedUpdate
}

// NewManager creates a panel update manager.
func NewManager(currentVersion, dataDir string) *Manager {
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
	releaseAPI := os.Getenv("PDAI_UPDATE_RELEASE_API")
	if releaseAPI == "" {
		releaseAPI = defaultReleaseAPI
	}
	return &Manager{
		currentVersion: strings.TrimSpace(currentVersion),
		releaseAPI:     releaseAPI,
		dataDir:        dataDir,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Check returns the current/latest release state without changing anything.
func (m *Manager) Check(ctx context.Context) (*ReleaseInfo, error) {
	rel, err := m.fetchLatest(ctx)
	if err != nil {
		return nil, err
	}
	return m.buildInfo(rel), nil
}

// Prepare downloads and stages the latest release for the current platform.
func (m *Manager) Prepare(ctx context.Context) (*ReleaseInfo, error) {
	rel, err := m.fetchLatest(ctx)
	if err != nil {
		return nil, err
	}

	info := m.buildInfo(rel)
	if !info.UpdateAvailable {
		return info, nil
	}
	if !info.CanUpdate {
		return info, errors.New(info.Reason)
	}

	asset := selectAsset(rel, info.RuntimeGOOS, info.RuntimeGOARCH)
	if asset == nil {
		info.CanUpdate = false
		info.Reason = "current release does not contain a matching binary for this platform"
		return info, errors.New(info.Reason)
	}

	stageDir := filepath.Join(m.dataDir, "updates", info.LatestVersion, info.RuntimeGOOS+"-"+info.RuntimeGOARCH)
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return info, fmt.Errorf("create update staging dir: %w", err)
	}

	if staged := m.getPrepared(); staged != nil && staged.Version == info.LatestVersion {
		if _, err := os.Stat(staged.AssetPath); err == nil {
			info.Prepared = true
			info.PreparedAt = staged.CreatedAt.Format(time.RFC3339)
			info.PreparedVersion = staged.Version
			info.PreparedPath = staged.AssetPath
			return info, nil
		}
	}

	archivePath := filepath.Join(stageDir, asset.Name)
	checksumPath := archivePath + ".sha256"
	binaryPath := filepath.Join(stageDir, "pdai")

	if err := m.downloadFile(ctx, asset.BrowserDownloadURL, archivePath); err != nil {
		return info, fmt.Errorf("download update package: %w", err)
	}
	if err := m.downloadFile(ctx, findChecksumURL(rel, asset.Name), checksumPath); err != nil {
		return info, fmt.Errorf("download checksum: %w", err)
	}
	if err := verifyArchiveChecksum(archivePath, checksumPath); err != nil {
		return info, err
	}
	if err := extractBinaryFromTarGz(archivePath, binaryPath); err != nil {
		return info, err
	}
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return info, fmt.Errorf("mark staged binary executable: %w", err)
	}

	m.setPrepared(&preparedUpdate{
		Version:   info.LatestVersion,
		AssetName: asset.Name,
		AssetPath: binaryPath,
		CreatedAt: time.Now(),
	})

	info.Prepared = true
	info.PreparedAt = time.Now().Format(time.RFC3339)
	info.PreparedVersion = info.LatestVersion
	info.PreparedPath = binaryPath
	return info, nil
}

// RestartWithHelper launches a temporary helper that waits for the current
// process to exit, swaps in the staged binary and starts the new one.
func (m *Manager) RestartWithHelper(ctx context.Context) error {
	staged := m.getPrepared()
	if staged == nil {
		return fmt.Errorf("no prepared update found")
	}
	if _, err := os.Stat(staged.AssetPath); err != nil {
		return fmt.Errorf("prepared binary missing: %w", err)
	}

	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	currentExe, err = filepath.Abs(currentExe)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	scriptPath, err := os.CreateTemp("", "pdai-update-helper-*.sh")
	if err != nil {
		return fmt.Errorf("create helper script: %w", err)
	}
	script := buildHelperScript()
	if _, err := scriptPath.WriteString(script); err != nil {
		scriptPath.Close()
		os.Remove(scriptPath.Name())
		return fmt.Errorf("write helper script: %w", err)
	}
	if err := scriptPath.Chmod(0755); err != nil {
		scriptPath.Close()
		os.Remove(scriptPath.Name())
		return fmt.Errorf("chmod helper script: %w", err)
	}
	if err := scriptPath.Close(); err != nil {
		os.Remove(scriptPath.Name())
		return fmt.Errorf("close helper script: %w", err)
	}

	logPath := filepath.Join(m.dataDir, "panel-update.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		os.Remove(scriptPath.Name())
		return fmt.Errorf("open update log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command("/bin/sh", scriptPath.Name(), currentExe, staged.AssetPath, fmt.Sprintf("%d", os.Getpid()), workingDir)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath.Name())
		return fmt.Errorf("start update helper: %w", err)
	}

	go func() {
		_ = cmd.Wait()
		_ = os.Remove(scriptPath.Name())
	}()

	return nil
}

func (m *Manager) buildInfo(rel *githubRelease) *ReleaseInfo {
	info := &ReleaseInfo{
		CurrentVersion: m.currentVersion,
		RuntimeGOOS:    runtime.GOOS,
		RuntimeGOARCH:  runtime.GOARCH,
	}
	if rel == nil {
		return info
	}

	info.ReleaseName = rel.Name
	info.TagName = rel.TagName
	info.LatestVersion = strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	info.PublishedAt = rel.PublishedAt
	info.Body = rel.Body
	info.HTMLURL = rel.HTMLURL

	asset := selectAsset(rel, runtime.GOOS, runtime.GOARCH)
	if asset != nil {
		info.AssetName = asset.Name
		info.AssetURL = asset.BrowserDownloadURL
		info.ChecksumURL = findChecksumURL(rel, asset.Name)
		info.CanUpdate = info.ChecksumURL != ""
		if !info.CanUpdate {
			info.Reason = "current release is missing the checksum asset"
		}
	} else {
		info.CanUpdate = false
		info.Reason = "current release does not contain a matching binary for this platform"
	}

	info.UpdateAvailable = isUpdateAvailable(m.currentVersion, info.LatestVersion)

	info.Prepared, info.PreparedAt, info.PreparedVersion, info.PreparedPath = m.preparedState(info.LatestVersion)
	if info.Prepared && info.PreparedVersion == "" {
		info.PreparedVersion = info.LatestVersion
	}
	return info
}

func (m *Manager) preparedState(latest string) (bool, string, string, string) {
	staged := m.getPrepared()
	if staged == nil {
		return false, "", "", ""
	}
	if latest != "" && staged.Version != latest {
		return false, "", "", ""
	}
	if _, err := os.Stat(staged.AssetPath); err != nil {
		return false, "", "", ""
	}
	return true, staged.CreatedAt.Format(time.RFC3339), staged.Version, staged.AssetPath
}

func (m *Manager) fetchLatest(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.releaseAPI, nil)
	if err != nil {
		return nil, fmt.Errorf("build release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PdaiPanel-Updater")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("fetch latest release: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rel githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode latest release: %w", err)
	}
	return &rel, nil
}

func (m *Manager) downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "PdaiPanel-Updater")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("download %s: http %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	tmpPath := path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("move downloaded file into place: %w", err)
	}
	return nil
}

func verifyArchiveChecksum(archivePath, checksumPath string) error {
	expectedBytes, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("read checksum: %w", err)
	}
	expected := strings.Fields(strings.TrimSpace(string(expectedBytes)))
	if len(expected) == 0 {
		return fmt.Errorf("checksum file %s is empty", checksumPath)
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash archive: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected[0]) {
		return fmt.Errorf("checksum mismatch for %s", archivePath)
	}
	return nil
}

func extractBinaryFromTarGz(archivePath, binaryPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != "pdai" {
			continue
		}
		out, err := os.Create(binaryPath)
		if err != nil {
			return fmt.Errorf("create staged binary: %w", err)
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			os.Remove(binaryPath)
			return fmt.Errorf("extract staged binary: %w", err)
		}
		if err := out.Close(); err != nil {
			os.Remove(binaryPath)
			return fmt.Errorf("close staged binary: %w", err)
		}
		return nil
	}
	return fmt.Errorf("pdai binary not found in archive")
}

func selectAsset(rel *githubRelease, goos, goarch string) *struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
} {
	if rel == nil {
		return nil
	}
	version := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	if version == "" {
		return nil
	}
	want := fmt.Sprintf("pdai-%s-%s-%s.tar.gz", version, goos, goarch)
	for i := range rel.Assets {
		if rel.Assets[i].Name == want {
			return &rel.Assets[i]
		}
	}
	return nil
}

func findChecksumURL(rel *githubRelease, assetName string) string {
	if rel == nil || assetName == "" {
		return ""
	}
	want := assetName + ".sha256"
	for i := range rel.Assets {
		if rel.Assets[i].Name == want {
			return rel.Assets[i].BrowserDownloadURL
		}
	}
	return ""
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return strings.ToLower(v)
}

func isUpdateAvailable(current, latest string) bool {
	current = normalizeVersion(current)
	latest = normalizeVersion(latest)
	if latest == "" {
		return false
	}
	if current == "" || current == "dev" {
		return true
	}
	if isTimestampBuildVersion(current) {
		if _, ok := semverParts(latest); ok {
			return true
		}
	}
	if cmp, ok := compareSemver(latest, current); ok {
		return cmp > 0
	}
	return current != latest
}

func isTimestampBuildVersion(v string) bool {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if len(v) < 8 || strings.Contains(v, ".") {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func compareSemver(a, b string) (int, bool) {
	ap, okA := semverParts(a)
	bp, okB := semverParts(b)
	if !okA || !okB {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if ap[i] > bp[i] {
			return 1, true
		}
		if ap[i] < bp[i] {
			return -1, true
		}
	}
	return 0, true
}

func semverParts(v string) ([3]int, bool) {
	var parts [3]int
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	raw := strings.Split(v, ".")
	if len(raw) == 0 || len(raw) > 3 {
		return parts, false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] == "" {
			return parts, false
		}
		for _, r := range raw[i] {
			if r < '0' || r > '9' {
				return parts, false
			}
			parts[i] = parts[i]*10 + int(r-'0')
		}
	}
	return parts, true
}

func buildHelperScript() string {
	return `#!/bin/sh
set -eu

OLD_BIN="$1"
NEW_BIN="$2"
PID="$3"
WORKDIR="$4"
BACKUP="${OLD_BIN}.bak"

wait_for_exit() {
  i=0
  while kill -0 "$PID" 2>/dev/null; do
    if [ "$i" -ge 30 ]; then
      break
    fi
    sleep 1
    i=$((i + 1))
  done
}

wait_for_exit

if kill -0 "$PID" 2>/dev/null; then
  kill "$PID" 2>/dev/null || true
  i=0
  while kill -0 "$PID" 2>/dev/null; do
    if [ "$i" -ge 20 ]; then
      break
    fi
    sleep 1
    i=$((i + 1))
  done
fi

if kill -0 "$PID" 2>/dev/null; then
  echo "panel process did not exit in time" >&2
  exit 1
fi

mkdir -p "$(dirname "$OLD_BIN")"
cp -f "$OLD_BIN" "$BACKUP"
chmod 755 "$NEW_BIN"
mv -f "$NEW_BIN" "$OLD_BIN"
chmod 755 "$OLD_BIN"

cd "$WORKDIR"
"$OLD_BIN" >/dev/null 2>&1 &
rm -f "$0" 2>/dev/null || true
`
}

func (m *Manager) getPrepared() *preparedUpdate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.prepared == nil {
		return nil
	}
	cp := *m.prepared
	return &cp
}

func (m *Manager) setPrepared(prepared *preparedUpdate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prepared == nil {
		m.prepared = nil
		return
	}
	cp := *prepared
	m.prepared = &cp
}
